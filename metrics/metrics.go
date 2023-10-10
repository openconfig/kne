// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package metrics handles anonymous usage metrics reporting.
package metrics

import (
	"context"
	"fmt"

	"cloud.google.com/go/pubsub"
	epb "github.com/openconfig/kne/proto/event"
	"github.com/pborman/uuid"
	"google.golang.org/protobuf/proto"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	log "k8s.io/klog/v2"
)

const (
	defaultProjectID = "gep-kne"
	defaultTopicID   = "kne-usage-events"
)

// Reporter is a client that reports KNE usage events using PubSub.
type Reporter struct {
	client *pubsub.Client
	topic  *pubsub.Topic
}

// NewReporter creates a new Reporter with a PubSub client for the given
// project and topic.
func NewReporter(ctx context.Context, projectID, topicID string) (*Reporter, error) {
	if projectID == "" {
		projectID = defaultProjectID
	}
	if topicID == "" {
		topicID = defaultTopicID
	}
	c, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create new pubsub client: %w", err)
	}
	t := c.Topic(topicID)
	ok, err := t.Exists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check if topic %q exists: %w", topicID, err)
	}
	if !ok {
		return nil, fmt.Errorf("topic %q does not exist in project %q", topicID, projectID)
	}
	return &Reporter{client: c, topic: t}, nil
}

// Close closes the Reporters underlying PubSub client and stops the underlying topic.
func (r *Reporter) Close() error {
	r.topic.Stop()
	return r.client.Close()
}

// publishEvent uses the underlying PubSub client to publish
// a marshaled KNEEvent proto message to the event topic.
func (r *Reporter) publishEvent(ctx context.Context, event *epb.KNEEvent) error {
	msg, err := proto.Marshal(event)
	if err != nil {
		return err
	}
	id, err := r.topic.Publish(ctx, &pubsub.Message{Data: msg}).Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}
	log.V(2).Infof("Published event %s with message ID: %s", event.GetUuid(), id)
	return nil
}

// ReportDeployClusterStart reports that a cluster deployment has started. The UUID of the
// reported event is returned if the event was successfully reported. Else, an error is returned.
func (r *Reporter) ReportDeployClusterStart(ctx context.Context, c *epb.Cluster) (string, error) {
	event := newEvent(uuid.New())
	event.Event = &epb.KNEEvent_DeployClusterStart{DeployClusterStart: &epb.DeployClusterStart{Cluster: c}}
	if err := r.publishEvent(ctx, event); err != nil {
		return "", fmt.Errorf("failed to report DeployClusterStart event with ID %s: %w", event.GetUuid(), err)
	}
	log.V(1).Infof("Reported DeployClusterStart event with ID %s", event.GetUuid())
	return event.GetUuid(), nil
}

// ReportDeployClusterEnd reports that a cluster deployment has ended. An error is returned if
// the event reporting failed.
func (r *Reporter) ReportDeployClusterEnd(ctx context.Context, id string, deployErr error) error {
	event := newEvent(id)
	end := &epb.DeployClusterEnd{}
	if deployErr != nil {
		end.Error = deployErr.Error()
	}
	event.Event = &epb.KNEEvent_DeployClusterEnd{DeployClusterEnd: end}
	if err := r.publishEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to report DeployClusterEnd event with ID %s: %w", event.GetUuid(), err)
	}
	log.V(1).Infof("Reported DeployClusterEnd event with ID %s", event.GetUuid())
	return nil
}

// ReportCreateTopologyStart reports that a topology creation has started. The UUID of the
// reported event is returned if the event was successfully reported. Else, an error is returned.
func (r *Reporter) ReportCreateTopologyStart(ctx context.Context, t *epb.Topology) (string, error) {
	event := newEvent(uuid.New())
	event.Event = &epb.KNEEvent_CreateTopologyStart{CreateTopologyStart: &epb.CreateTopologyStart{Topology: t}}
	if err := r.publishEvent(ctx, event); err != nil {
		return "", fmt.Errorf("failed to report CreateTopologyStart event with ID %s: %w", event.GetUuid(), err)
	}
	log.V(1).Infof("Reported CreateTopologyStart event with ID %s", event.GetUuid())
	return event.GetUuid(), nil
}

// ReportCreateTopologyEnd reports that a topology creation has ended. An error is returned if
// the event reporting failed.
func (r *Reporter) ReportCreateTopologyEnd(ctx context.Context, id string, createErr error) error {
	event := newEvent(id)
	end := &epb.CreateTopologyEnd{}
	if createErr != nil {
		end.Error = createErr.Error()
	}
	event.Event = &epb.KNEEvent_CreateTopologyEnd{CreateTopologyEnd: end}
	if err := r.publishEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to report CreateTopologyEnd event with ID %s: %w", event.GetUuid(), err)
	}
	log.V(1).Infof("Reported CreateTopologyEnd event with ID %s", event.GetUuid())
	return nil
}

func newEvent(id string) *epb.KNEEvent {
	if id == "" {
		id = uuid.New()
	}
	return &epb.KNEEvent{
		Uuid:      id,
		Timestamp: timestamppb.Now(),
	}
}
