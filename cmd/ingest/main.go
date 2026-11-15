// Ingest Lambda — receives errors, groups them, stores raw payloads,
// and kicks off the alert state machine when needed.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"faultline/internal"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
)

var (
	store     *internal.Store
	s3Client  *s3.Client
	sfnClient *sfn.Client
)

func init() {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("load AWS config: %v", err)
	}

	store = &internal.Store{
		DDB:              dynamodb.NewFromConfig(cfg),
		ErrorsTable:      os.Getenv("ERRORS_TABLE"),
		OccurrencesTable: os.Getenv("OCCURRENCES_TABLE"),
	}
	s3Client = s3.NewFromConfig(cfg)
	sfnClient = sfn.NewFromConfig(cfg)
}

// groupID produces a short, deterministic hash for (message, source).
func groupID(message, source string) string {
	h := sha256.Sum256([]byte(message + "|" + source))
	return fmt.Sprintf("%x", h[:8]) // 16-char hex
}

func handler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var ev internal.ErrorEvent
	if err := json.Unmarshal([]byte(req.Body), &ev); err != nil {
		return respond(400, map[string]string{"error": "invalid JSON body"})
	}

	if ev.Message == "" || ev.Source == "" {
		return respond(400, map[string]string{"error": "message and source are required"})
	}
	if ev.Level == "" {
		ev.Level = "error"
	}

	now := time.Now().UTC()
	if ev.Timestamp == "" {
		ev.Timestamp = now.Format(time.RFC3339Nano)
	}

	gid := groupID(ev.Message, ev.Source)

	// 1. Upsert the error group (atomic counter).
	count, err := store.UpsertErrorGroup(ctx, gid, ev.Message, ev.Source, ev.Level)
	if err != nil {
		log.Printf("ERROR upsert: %v", err)
		return respond(500, map[string]string{"error": "failed to store error group"})
	}

	// 2. Store raw payload in S3.
	rawKey := fmt.Sprintf("%s/%s.json", gid, ev.Timestamp)
	rawBody, _ := json.Marshal(ev)
	_, s3Err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(os.Getenv("RAW_BUCKET")),
		Key:         aws.String(rawKey),
		Body:        bytes.NewReader(rawBody),
		ContentType: aws.String("application/json"),
	})
	if s3Err != nil {
		log.Printf("WARN s3 put: %v", s3Err)
	}

	// 3. Store occurrence in DynamoDB.
	occ := internal.ErrorOccurrence{
		GroupID:    gid,
		Timestamp:  ev.Timestamp,
		Level:      ev.Level,
		Stacktrace: ev.Stacktrace,
		Metadata:   ev.Metadata,
	}
	if s3Err == nil {
		occ.S3Key = rawKey
	}
	if err := store.PutOccurrence(ctx, occ); err != nil {
		log.Printf("WARN occurrence: %v", err)
	}

	// 4. Trigger the alert state machine on the 1st hit, then every 10th.
	if count == 1 || count%10 == 0 {
		alertInput := internal.AlertInput{
			GroupID:   gid,
			Message:   ev.Message,
			Source:    ev.Source,
			Level:     ev.Level,
			Count:     count,
			FirstSeen: ev.Timestamp,
		}
		inputJSON, _ := json.Marshal(alertInput)

		_, sfnErr := sfnClient.StartExecution(ctx, &sfn.StartExecutionInput{
			StateMachineArn: aws.String(os.Getenv("STATE_MACHINE_ARN")),
			Input:           aws.String(string(inputJSON)),
		})
		if sfnErr != nil {
			log.Printf("WARN step functions: %v", sfnErr)
		}
	}

	return respond(200, map[string]interface{}{
		"group_id": gid,
		"count":    count,
		"status":   "ingested",
	})
}

func respond(status int, body interface{}) (events.APIGatewayProxyResponse, error) {
	b, _ := json.Marshal(body)
	return events.APIGatewayProxyResponse{
		StatusCode: status,
		Headers: map[string]string{
			"Content-Type":                 "application/json",
			"Access-Control-Allow-Origin":  "*",
			"Access-Control-Allow-Methods": "POST,OPTIONS",
		},
		Body: string(b),
	}, nil
}

func main() {
	lambda.Start(handler)
}
