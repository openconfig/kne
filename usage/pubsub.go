package usage

import (
	"context"
	"fmt"

	"cloud.google.com/go/pubsub"
	epb "github.com/openconfig/kne/proto/event"
	"google.golang.org/protobuf/proto"
	log "k8s.io/klog/v2"
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
	msg, err := proto.Marshal(event)
	if err != nil {
		return err
	}
	t := client.Topic(topicID)
	res := t.Publish(ctx, &pubsub.Message{Data: msg})
	id, err := res.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}
	log.V(2).Infof("Published event %s with message ID: %s", event.GetUuid(), id)
	return nil
}
