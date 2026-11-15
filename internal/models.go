// Package internal holds shared types and helpers for FaultLine.
package internal

// ErrorEvent is the payload sent by client applications.
type ErrorEvent struct {
	Message    string            `json:"message"`
	Source     string            `json:"source"`
	Level      string            `json:"level,omitempty"`
	Stacktrace string            `json:"stacktrace,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Timestamp  string            `json:"timestamp,omitempty"`
}

// ErrorGroup represents a group of similar errors stored in DynamoDB.
type ErrorGroup struct {
	GroupID      string `json:"group_id"       dynamodbav:"GroupID"`
	Message      string `json:"message"        dynamodbav:"Message"`
	Source       string `json:"source"         dynamodbav:"Source"`
	Level        string `json:"level"          dynamodbav:"Level"`
	Count        int    `json:"count"          dynamodbav:"Count"`
	FirstSeen    string `json:"first_seen"     dynamodbav:"FirstSeen"`
	LastSeen     string `json:"last_seen"      dynamodbav:"LastSeen"`
	LastNotified string `json:"last_notified,omitempty" dynamodbav:"LastNotified,omitempty"`
	Status       string `json:"status"         dynamodbav:"Status"`
}

// ErrorOccurrence is a single error event linked to a group.
type ErrorOccurrence struct {
	GroupID    string            `json:"group_id"              dynamodbav:"GroupID"`
	Timestamp  string            `json:"timestamp"             dynamodbav:"Timestamp"`
	Level      string            `json:"level"                 dynamodbav:"Level"`
	Stacktrace string            `json:"stacktrace,omitempty"  dynamodbav:"Stacktrace,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"    dynamodbav:"Metadata,omitempty"`
	S3Key      string            `json:"s3_key,omitempty"      dynamodbav:"S3Key,omitempty"`
}

// AlertInput is the payload sent to the Step Functions state machine.
type AlertInput struct {
	GroupID   string `json:"group_id"`
	Message   string `json:"message"`
	Source    string `json:"source"`
	Level     string `json:"level"`
	Count     int    `json:"count"`
	FirstSeen string `json:"first_seen"`
}

// AlerterRequest is the input the alerter Lambda receives from Step Functions.
type AlerterRequest struct {
	Action             string `json:"action"`
	GroupID            string `json:"group_id"`
	SuppressionMinutes int    `json:"suppression_minutes,omitempty"`
}

// AlerterResponse is what the alerter Lambda returns to Step Functions.
type AlerterResponse struct {
	ShouldAlert bool   `json:"should_alert"`
	Reason      string `json:"reason"`
}
