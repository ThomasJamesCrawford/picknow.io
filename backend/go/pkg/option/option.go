package option

import (
	"context"
	"fmt"
	"os"
	"picker/backend/go/pkg/dynamodbTypes"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/twinj/uuid"
)

type SelectOptionRequest struct {
	Name string `json:"name" binding:"required,min=1,max=1500"`
}

type CreateOptionRequest struct {
	Option string `json:"option" binding:"required,min=1,max=1500"`
}

type Option struct {
	// DynamoDB
	PK   string `dynamodbav:"PK" json:"-"`
	SK   string `dynamodbav:"SK" json:"-"`
	Type string `dynamodbav:"type" json:"-"`

	// Public
	ID             string  `json:"id" dynamodbav:"id"`
	RoomID         string  `json:"roomID" dynamodbav:"roomID"`
	Value          string  `json:"value" dynamodbav:"value"`
	Available      bool    `dynamodbav:"-" json:"available"`
	SelectedByName *string `dynamodbav:"selectedByName,omitEmpty" json:"selectedByName,omitempty"`

	// Private
	SelectedByID *string `dynamodbav:"selectedByID,omitEmpty" json:"-"`
	OwnedByID    string  `dynamodbav:"ownedByID" json:"-"`
}

type PublicOption struct {
	ID             string  `json:"id"`
	RoomID         string  `json:"roomID"`
	Value          string  `json:"value"`
	Available      bool    `json:"available"`
	SelectedByMeAs *string `json:"selectedByMeAs,omitempty"`
}

func (option Option) getPublic(userID string) PublicOption {
	var selectedByMeAs *string
	if option.SelectedByID != nil && userID == *option.SelectedByID {
		selectedByMeAs = option.SelectedByName
	}

	return PublicOption{
		ID:             option.ID,
		RoomID:         option.RoomID,
		Value:          option.Value,
		Available:      option.Available,
		SelectedByMeAs: selectedByMeAs,
	}
}

func MapToPublic(options []Option, userID string) []PublicOption {
	out := make([]PublicOption, len(options))

	for i, opt := range options {
		out[i] = opt.getPublic(userID)
	}

	return out
}

func SelectOption(optionID string, userID string, roomID string, selectOptionRequest SelectOptionRequest, client *dynamodb.Client) (*PublicOption, error) {
	res, err := client.UpdateItem(context.TODO(), &dynamodb.UpdateItemInput{
		TableName: aws.String(os.Getenv("table")),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: fmt.Sprintf("ROOM#%s", roomID)},
			"SK": &types.AttributeValueMemberS{Value: fmt.Sprintf("ROOM_OPTION#%s", optionID)},
		},
		UpdateExpression: aws.String("set selectedByID = :userID, selectedByName = :name"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":userID": &types.AttributeValueMemberS{Value: userID},
			":name":   &types.AttributeValueMemberS{Value: selectOptionRequest.Name},
			":null":   &types.AttributeValueMemberNULL{Value: true},
		},
		ConditionExpression: aws.String("(attribute_not_exists(selectedByID) or selectedByID = :null) and attribute_exists(PK) and attribute_exists(SK)"),
		ReturnValues:        types.ReturnValueAllNew,
	})

	if err != nil {
		return nil, err
	}

	updatedOption := Unmarshal(res.Attributes).getPublic(userID)

	return &updatedOption, nil
}

func UnselectOption(optionID string, userID string, roomID string, client *dynamodb.Client) (*PublicOption, error) {
	res, err := client.UpdateItem(context.TODO(), &dynamodb.UpdateItemInput{
		TableName: aws.String(os.Getenv("table")),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: fmt.Sprintf("ROOM#%s", roomID)},
			"SK": &types.AttributeValueMemberS{Value: fmt.Sprintf("ROOM_OPTION#%s", optionID)},
		},
		UpdateExpression: aws.String("set selectedByID = :null, selectedByName = :null"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":userID": &types.AttributeValueMemberS{Value: userID},
			":null":   &types.AttributeValueMemberNULL{Value: true},
		},
		ConditionExpression: aws.String("selectedByID = :userID and attribute_exists(PK) and attribute_exists(SK)"),
		ReturnValues:        types.ReturnValueAllNew,
	})

	if err != nil {
		return nil, err
	}

	updatedOption := Unmarshal(res.Attributes).getPublic(userID)

	return &updatedOption, nil
}

func Delete(optionID string, userID string, roomID string, client *dynamodb.Client) (*Option, error) {
	res, err := client.DeleteItem(context.TODO(), &dynamodb.DeleteItemInput{
		TableName: aws.String(os.Getenv("table")),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: fmt.Sprintf("ROOM#%s", roomID)},
			"SK": &types.AttributeValueMemberS{Value: fmt.Sprintf("ROOM_OPTION#%s", optionID)},
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":userID": &types.AttributeValueMemberS{Value: userID},
		},
		ConditionExpression: aws.String("ownedByID = :userID"),
		ReturnValues:        types.ReturnValueAllOld,
	})

	if err != nil {
		return nil, err
	}

	updatedOption := Unmarshal(res.Attributes)

	return &updatedOption, nil
}

func NewOption(option string, userID string, roomID string) Option {
	optionID := uuid.NewV4().String()

	return Option{
		PK: fmt.Sprintf("ROOM#%s", roomID),
		// This lets us use a BEGINS_WITH in our single table to pull in a room and all the options with one query
		SK:   fmt.Sprintf("ROOM_OPTION#%s", optionID),
		Type: dynamodbTypes.Option,

		ID:     optionID,
		RoomID: roomID,
		Value:  option,

		SelectedByID:   nil,
		SelectedByName: nil,
		OwnedByID:      userID,
	}
}

func batchWriteOptionChunk(chunk []*Option, client *dynamodb.Client, wg *sync.WaitGroup) {
	defer wg.Done()

	var items []types.WriteRequest

	for _, option := range chunk {
		item, err := attributevalue.MarshalMap(option)

		if err != nil {
			panic(err)
		}

		items = append(items, types.WriteRequest{
			PutRequest: &types.PutRequest{
				Item: item,
			},
		})
	}

	request := &dynamodb.BatchWriteItemInput{
		RequestItems: map[string][]types.WriteRequest{
			os.Getenv("table"): items,
		},
	}

	_, err := client.BatchWriteItem(context.TODO(), request)

	if err != nil {
		panic(err)
	}
}

func BatchWriteOptions(options []*Option, client *dynamodb.Client) error {
	// BatchWriteItem does a max of 25 items
	chunkedOptions := chunk(options, 25)

	var wg sync.WaitGroup

	for _, chunk := range chunkedOptions {
		wg.Add(1)

		// Process each chunk async
		go batchWriteOptionChunk(chunk, client, &wg)
	}

	wg.Wait()

	return nil
}

// https://freshman.tech/snippets/go/split-slice-into-chunks/#loop-through-the-number-of-chunks
func chunk(options []*Option, chunkSize int) [][]*Option {
	var chunks [][]*Option

	for i := 0; i < len(options); i += chunkSize {
		end := i + chunkSize

		if end > len(options) {
			end = len(options)
		}

		chunks = append(chunks, options[i:end])
	}

	return chunks
}

func Unmarshal(item map[string]types.AttributeValue) Option {
	option := &Option{}

	if err := attributevalue.UnmarshalMap(item, option); err != nil {
		panic(err)
	}

	option.Available = option.SelectedByID == nil

	return *option
}
