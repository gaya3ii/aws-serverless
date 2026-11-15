// Alerter Lambda — helper used by the Step Functions state machine.
// Actions:
//   - check_suppression: should we alert or is it within the quiet window?
//   - update_notified:   stamp the group as notified right now.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"faultline/internal"

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
		DDB:         dynamodb.NewFromConfig(cfg),
		ErrorsTable: os.Getenv("ERRORS_TABLE"),
	}
}

func handler(ctx context.Context, req internal.AlerterRequest) (internal.AlerterResponse, error) {
	switch req.Action {
	case "check_suppression":
		return checkSuppression(ctx, req)
	case "update_notified":
		return updateNotified(ctx, req)
	default:
		return internal.AlerterResponse{
			ShouldAlert: false,
			Reason:      "unknown action: " + req.Action,
		}, nil
	}
}

func checkSuppression(ctx context.Context, req internal.AlerterRequest) (internal.AlerterResponse, error) {
	lastNotified, err := store.GetLastNotified(ctx, req.GroupID)
	if err != nil {
		log.Printf("WARN get last notified: %v", err)
		// On error, alert anyway to be safe.
		return internal.AlerterResponse{ShouldAlert: true, Reason: "check failed, alerting by default"}, nil
	}

	// Never notified before → alert.
	if lastNotified == "" {
		return internal.AlerterResponse{ShouldAlert: true, Reason: "first alert for this group"}, nil
	}

	lastTime, err := time.Parse(time.RFC3339, lastNotified)
	if err != nil {
		return internal.AlerterResponse{ShouldAlert: true, Reason: "invalid timestamp, alerting"}, nil
	}

	window := req.SuppressionMinutes
	if window <= 0 {
		window = 30
	}

	if time.Since(lastTime) > time.Duration(window)*time.Minute {
		return internal.AlerterResponse{ShouldAlert: true, Reason: "suppression window expired"}, nil
	}

	return internal.AlerterResponse{
		ShouldAlert: false,
		Reason:      "suppressed — last notified " + lastNotified,
	}, nil
}

func updateNotified(ctx context.Context, req internal.AlerterRequest) (internal.AlerterResponse, error) {
	if err := store.UpdateLastNotified(ctx, req.GroupID); err != nil {
		log.Printf("ERROR update notified: %v", err)
		return internal.AlerterResponse{Reason: "failed to update"}, err
	}
	return internal.AlerterResponse{Reason: "updated"}, nil
}

func main() {
	lambda.Start(handler)
}
