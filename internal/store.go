package internal

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Store wraps DynamoDB operations for error groups and occurrences.
type Store struct {
	DDB              *dynamodb.Client
	ErrorsTable      string
	OccurrencesTable string
}

// UpsertErrorGroup creates or updates an error group atomically.
// Returns the new total count after the increment.
func (s *Store) UpsertErrorGroup(ctx context.Context, groupID, message, source, level string) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	out, err := s.DDB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.ErrorsTable),
		Key: map[string]types.AttributeValue{
			"GroupID": &types.AttributeValueMemberS{Value: groupID},
		},
		UpdateExpression: aws.String(
			"SET #msg = :msg, #src = :src, #lvl = :lvl, LastSeen = :now, " +
				"FirstSeen = if_not_exists(FirstSeen, :now), " +
				"#st = if_not_exists(#st, :active) " +
				"ADD #cnt :one",
		),
		ExpressionAttributeNames: map[string]string{
			"#msg": "Message",
			"#src": "Source",
			"#lvl": "Level",
			"#cnt": "Count",
			"#st":  "Status",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":msg":    &types.AttributeValueMemberS{Value: message},
			":src":    &types.AttributeValueMemberS{Value: source},
			":lvl":    &types.AttributeValueMemberS{Value: level},
			":now":    &types.AttributeValueMemberS{Value: now},
			":one":    &types.AttributeValueMemberN{Value: "1"},
			":active": &types.AttributeValueMemberS{Value: "active"},
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert error group: %w", err)
	}

	var group ErrorGroup
	if err := attributevalue.UnmarshalMap(out.Attributes, &group); err != nil {
		return 0, fmt.Errorf("unmarshal updated group: %w", err)
	}

	return group.Count, nil
}

// PutOccurrence writes a single error occurrence.
func (s *Store) PutOccurrence(ctx context.Context, occ ErrorOccurrence) error {
	item, err := attributevalue.MarshalMap(occ)
	if err != nil {
		return fmt.Errorf("marshal occurrence: %w", err)
	}

	_, err = s.DDB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.OccurrencesTable),
		Item:      item,
	})
	return err
}

// ListErrorGroups returns every error group (scan — fine for dashboards).
func (s *Store) ListErrorGroups(ctx context.Context) ([]ErrorGroup, error) {
	out, err := s.DDB.Scan(ctx, &dynamodb.ScanInput{
		TableName: aws.String(s.ErrorsTable),
	})
	if err != nil {
		return nil, fmt.Errorf("scan error groups: %w", err)
	}

	var groups []ErrorGroup
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &groups); err != nil {
		return nil, fmt.Errorf("unmarshal groups: %w", err)
	}
	return groups, nil
}

// GetErrorGroup returns a single group by ID, or nil if not found.
func (s *Store) GetErrorGroup(ctx context.Context, groupID string) (*ErrorGroup, error) {
	out, err := s.DDB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.ErrorsTable),
		Key: map[string]types.AttributeValue{
			"GroupID": &types.AttributeValueMemberS{Value: groupID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get error group: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}

	var group ErrorGroup
	if err := attributevalue.UnmarshalMap(out.Item, &group); err != nil {
		return nil, fmt.Errorf("unmarshal group: %w", err)
	}
	return &group, nil
}

// GetOccurrences returns the most recent occurrences for a group.
func (s *Store) GetOccurrences(ctx context.Context, groupID string, limit int32) ([]ErrorOccurrence, error) {
	out, err := s.DDB.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.OccurrencesTable),
		KeyConditionExpression: aws.String("GroupID = :gid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":gid": &types.AttributeValueMemberS{Value: groupID},
		},
		ScanIndexForward: aws.Bool(false), // newest first
		Limit:            aws.Int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("query occurrences: %w", err)
	}

	var occs []ErrorOccurrence
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &occs); err != nil {
		return nil, fmt.Errorf("unmarshal occurrences: %w", err)
	}
	return occs, nil
}

// GetLastNotified returns the LastNotified timestamp for a group.
func (s *Store) GetLastNotified(ctx context.Context, groupID string) (string, error) {
	group, err := s.GetErrorGroup(ctx, groupID)
	if err != nil {
		return "", err
	}
	if group == nil {
		return "", nil
	}
	return group.LastNotified, nil
}

// UpdateLastNotified stamps the current time as LastNotified.
func (s *Store) UpdateLastNotified(ctx context.Context, groupID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.DDB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.ErrorsTable),
		Key: map[string]types.AttributeValue{
			"GroupID": &types.AttributeValueMemberS{Value: groupID},
		},
		UpdateExpression: aws.String("SET LastNotified = :now"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":now": &types.AttributeValueMemberS{Value: now},
		},
	})
	return err
}
