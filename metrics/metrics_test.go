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

package metrics

import (
	"context"
	"fmt"
	"testing"

	"cloud.google.com/go/pubsub"
	"cloud.google.com/go/pubsub/pstest"
	epb "github.com/openconfig/kne/proto/event"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// newTestReporter creates a new Reporter for testing with a fake PubSub
// configuration. The Reporter, the PubSub test server, and a cleanup
// function are returned.
func newTestReporter(t *testing.T, ctx context.Context) (*Reporter, *pstest.Server, func()) {
	t.Helper()
	// Start a fake server running locally.
	srv := pstest.NewServer()
	// Connect to the server without using TLS.
	conn, err := grpc.Dial(srv.Addr, grpc.WithInsecure())
	if err != nil {
		srv.Close()
		t.Fatalf("failed to start fake PubSub server: %v", err)
	}
	// Use the connection when creating a pubsub client.
	client, err := pubsub.NewClient(ctx, "test-project", option.WithGRPCConn(conn))
	if err != nil {
		conn.Close()
		srv.Close()
		t.Fatalf("failed to create fake PubSub client: %v", err)
	}
	topic, err := client.CreateTopic(ctx, "test-topic")
	if err != nil {
		client.Close()
		conn.Close()
		srv.Close()
		t.Fatalf("failed to create fake PubSub topic: %v", err)
	}
	return &Reporter{client: client, topic: topic}, srv, func() {
		conn.Close()
		srv.Close()
	}
}

func TestReportDeployClusterStart(t *testing.T) {
	ctx := context.Background()

	r, srv, cleanup := newTestReporter(t, ctx)
	defer cleanup()
	defer r.Close()

	id, err := r.ReportDeployClusterStart(ctx, &epb.Cluster{Cluster: epb.Cluster_CLUSTER_TYPE_EXTERNAL})
	if err != nil {
		t.Fatalf("failed to report DeployClusterStart event: %v", err)
	}

	msgs := srv.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in pubsub, got %v", len(msgs))
	}
	e := &epb.KNEEvent{}
	if err := proto.Unmarshal(msgs[0].Data, e); err != nil {
		t.Fatalf("failed to parse message into event: %v", err)
	}
	if e.Uuid != id {
		t.Errorf("event does not have the correct UUID, got %v, want %v", e.Uuid, id)
	}
	if e.Timestamp == nil {
		t.Errorf("event has nil timestamp")
	}
	if _, ok := e.Event.(*epb.KNEEvent_DeployClusterStart); !ok {
		t.Fatalf("event is not a DeployClusterStart event")
	}
	te := e.GetDeployClusterStart()
	if te.Cluster.Cluster != epb.Cluster_CLUSTER_TYPE_EXTERNAL {
		t.Errorf("event has wrong cluster type, got %v, want %v", te.Cluster.Cluster, epb.Cluster_CLUSTER_TYPE_EXTERNAL)
	}
}

func TestReportDeployClusterEnd(t *testing.T) {
	ctx := context.Background()

	r, srv, cleanup := newTestReporter(t, ctx)
	defer cleanup()
	defer r.Close()

	id := "fake-uuid"
	eventErr := fmt.Errorf("failed to deploy cluster")
	if err := r.ReportDeployClusterEnd(ctx, id, eventErr); err != nil {
		t.Fatalf("failed to report DeployClusterEnd event: %v", err)
	}

	msgs := srv.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in pubsub, got %v", len(msgs))
	}
	e := &epb.KNEEvent{}
	if err := proto.Unmarshal(msgs[0].Data, e); err != nil {
		t.Fatalf("failed to parse message into event: %v", err)
	}
	if e.Uuid != id {
		t.Errorf("event does not have the correct UUID, got %v, want %v", e.Uuid, id)
	}
	if e.Timestamp == nil {
		t.Errorf("event has nil timestamp")
	}
	if _, ok := e.Event.(*epb.KNEEvent_DeployClusterEnd); !ok {
		t.Fatalf("event is not a DeployClusterEnd event")
	}
	te := e.GetDeployClusterEnd()
	if te.Error != eventErr.Error() {
		t.Errorf("event has wrong error message, got %v, want %v", te.Error, eventErr.Error())
	}
}

func TestReportCreateTopologyStart(t *testing.T) {
	ctx := context.Background()

	r, srv, cleanup := newTestReporter(t, ctx)
	defer cleanup()
	defer r.Close()

	id, err := r.ReportCreateTopologyStart(ctx, &epb.Topology{LinkCount: 3})
	if err != nil {
		t.Fatalf("failed to report CreateTopologyStart event: %v", err)
	}

	msgs := srv.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in pubsub, got %v", len(msgs))
	}
	e := &epb.KNEEvent{}
	if err := proto.Unmarshal(msgs[0].Data, e); err != nil {
		t.Fatalf("failed to parse message into event: %v", err)
	}
	if e.Uuid != id {
		t.Errorf("event does not have the correct UUID, got %v, want %v", e.Uuid, id)
	}
	if e.Timestamp == nil {
		t.Errorf("event has nil timestamp")
	}
	if _, ok := e.Event.(*epb.KNEEvent_CreateTopologyStart); !ok {
		t.Fatalf("event is not a CreateTopologyStart event")
	}
	te := e.GetCreateTopologyStart()
	if te.Topology.LinkCount != 3 {
		t.Errorf("event has wrong link count, got %v, want 3", te.Topology.LinkCount)
	}
}

func TestReportCreateTopologyEnd(t *testing.T) {
	ctx := context.Background()

	r, srv, cleanup := newTestReporter(t, ctx)
	defer cleanup()
	defer r.Close()

	id := "fake-uuid"
	eventErr := fmt.Errorf("failed to create topology")
	if err := r.ReportCreateTopologyEnd(ctx, id, eventErr); err != nil {
		t.Fatalf("failed to report CreateTopologyEnd event: %v", err)
	}

	msgs := srv.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in pubsub, got %v", len(msgs))
	}
	e := &epb.KNEEvent{}
	if err := proto.Unmarshal(msgs[0].Data, e); err != nil {
		t.Fatalf("failed to parse message into event: %v", err)
	}
	if e.Uuid != id {
		t.Errorf("event does not have the correct UUID, got %v, want %v", e.Uuid, id)
	}
	if e.Timestamp == nil {
		t.Errorf("event has nil timestamp")
	}
	if _, ok := e.Event.(*epb.KNEEvent_CreateTopologyEnd); !ok {
		t.Fatalf("event is not a CreateTopologyEnd event")
	}
	te := e.GetCreateTopologyEnd()
	if te.Error != eventErr.Error() {
		t.Errorf("event has wrong error message, got %v, want %v", te.Error, eventErr.Error())
	}
}
