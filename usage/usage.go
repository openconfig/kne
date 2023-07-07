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

// Package usage handles anonymous usage reporting.
package usage

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
	projectID = "gep-kne"
	topicID   = "kne-usage-events"
)

// Reporter is a client that reports KNE usage events using PubSub.
type Reporter struct {
	pubsubClient *pubsub.Client
}

// NewReporter creates a new Reporter with a PubSub client.
func NewReporter(ctx context.Context) (*Reporter, error) {
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create new pubsub client: %w", err)
	}
	return &Reporter{pubsubClient: client}, nil
}

// Close closes a Reporter and the underlying PubSub client.
func (r *Reporter) Close() error {
	return r.pubsubClient.Close()
}

// publishEvent uses the underlying PubSub client to publish
// a marshaled KNEEvent proto message to the event topic.
func (r *Reporter) publishEvent(ctx context.Context, event *epb.KNEEvent) error {
	msg, err := proto.Marshal(event)
	if err != nil {
		return err
	}
	t := r.pubsubClient.Topic(topicID)
	res := t.Publish(ctx, &pubsub.Message{Data: msg})
	id, err := res.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}
	log.V(2).Infof("Published event %s with message ID: %s", event.GetUuid(), id)
	return nil
}

// ReportDeployClusterStart reports that a cluster deployment has started. The UUID of the
// reported event is returned along with an error if the event reporting failed.
func (r *Reporter) ReportDeployClusterStart(ctx context.Context, c *epb.Cluster) (string, error) {
	event := newEvent()
	event.Event = &epb.KNEEvent_DeployClusterStart{DeployClusterStart: &epb.DeployClusterStart{Cluster: c}}
	if err := r.publishEvent(ctx, event); err != nil {
		return "", fmt.Errorf("failed to report DeployClusterStart event with ID %s: %w", event.GetUuid(), err)
	}
	log.V(1).Infof("Reported DeployClusterStart event with ID %s", event.GetUuid())
	return event.GetUuid(), nil
}

// ReportDeployClusterEnd reports that a cluster deployment has ended.
func (r *Reporter) ReportDeployClusterEnd(ctx context.Context, id string, deployErr error) error {
	event := newEvent()
	event.Uuid = id
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
// reported event is returned along with an error if the event reporting failed.
func (r *Reporter) ReportCreateTopologyStart(ctx context.Context, t *epb.Topology) (string, error) {
	event := newEvent()
	event.Event = &epb.KNEEvent_CreateTopologyStart{CreateTopologyStart: &epb.CreateTopologyStart{Topology: t}}
	if err := r.publishEvent(ctx, event); err != nil {
		return "", fmt.Errorf("failed to report CreateTopologyStart event with ID %s: %w", event.GetUuid(), err)
	}
	log.V(1).Infof("Reported CreateTopologyStart event with ID %s", event.GetUuid())
	return event.GetUuid(), nil
}

// ReportCreateTopologyEnd reports that a topology creation has ended.
func (r *Reporter) ReportCreateTopologyEnd(ctx context.Context, id string, createErr error) error {
	event := newEvent()
	event.Uuid = id
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

func newEvent() *epb.KNEEvent {
	return &epb.KNEEvent{
		Uuid:      uuid.New(),
		Timestamp: timestamppb.Now(),
	}
}
