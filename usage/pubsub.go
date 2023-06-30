package usage

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/encoding/prototext"
	log "k8s.io/klog/v2"
	"cloud.google.com/go/pubsub"
	epb "github.com/openconfig/kne/proto/event"
)

const (
	projectID = "gep-kne"
	topicID   = "kne-test"
)

func publishEvent(ctx context.Context, event *epb.KNEEvent) error {
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return fmt.Errorf("failed to create new pubsub client: %w", err)
	}
	defer client.Close()
	msg, err := prototext.Marshal(event)
	if err != nil {
		return err
	}
	t := client.Topic(topicID)
	res := t.Publish(ctx, &pubsub.Message{Data: []byte(msg)})
	id, err := res.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}
	log.V(2).Infof("Published event %s with message ID: %s", event.GetUuid(), id)
	return nil
}
