// Query Lambda — serves GET /errors and GET /errors/{id}.
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"faultline/internal"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

var store *internal.Store

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
}

func handler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Route: GET /errors/{id}
	if id, ok := req.PathParameters["id"]; ok && id != "" {
		return getErrorGroup(ctx, id)
	}

	// Route: GET /errors
	return listErrorGroups(ctx)
}

func listErrorGroups(ctx context.Context) (events.APIGatewayProxyResponse, error) {
	groups, err := store.ListErrorGroups(ctx)
	if err != nil {
		log.Printf("ERROR list: %v", err)
		return respond(500, map[string]string{"error": "failed to list errors"})
	}

	if groups == nil {
		groups = []internal.ErrorGroup{}
	}

	return respond(200, map[string]interface{}{
		"errors": groups,
		"count":  len(groups),
	})
}

func getErrorGroup(ctx context.Context, id string) (events.APIGatewayProxyResponse, error) {
	group, err := store.GetErrorGroup(ctx, id)
	if err != nil {
		log.Printf("ERROR get: %v", err)
		return respond(500, map[string]string{"error": "failed to get error group"})
	}
	if group == nil {
		return respond(404, map[string]string{"error": "error group not found"})
	}

	occurrences, err := store.GetOccurrences(ctx, id, 25)
	if err != nil {
		log.Printf("WARN occurrences: %v", err)
	}
	if occurrences == nil {
		occurrences = []internal.ErrorOccurrence{}
	}

	return respond(200, map[string]interface{}{
		"group":       group,
		"occurrences": occurrences,
	})
}

func respond(status int, body interface{}) (events.APIGatewayProxyResponse, error) {
	b, _ := json.Marshal(body)
	return events.APIGatewayProxyResponse{
		StatusCode: status,
		Headers: map[string]string{
			"Content-Type":                 "application/json",
			"Access-Control-Allow-Origin":  "*",
			"Access-Control-Allow-Methods": "GET,OPTIONS",
		},
		Body: string(b),
	}, nil
}

func main() {
	lambda.Start(handler)
}
