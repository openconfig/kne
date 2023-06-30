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

	epb "github.com/openconfig/kne/proto/event"
	tpb "github.com/openconfig/kne/proto/topo"
	"github.com/pborman/uuid"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	log "k8s.io/klog/v2"
)

func newEvent() *epb.KNEEvent {
	return &epb.KNEEvent{
		Uuid:      uuid.New(),
		Timestamp: timestamppb.Now(),
	}
}

// ReportDeployClusterStart reports that a cluster deployment has started.
func ReportDeployClusterStart(ctx context.Context, c *epb.Cluster) string {
	event := newEvent()
	event.Event = &epb.KNEEvent_DeployClusterStart{DeployClusterStart: &epb.DeployClusterStart{Cluster: c}}
	if err := publishEvent(ctx, event); err != nil {
		log.Warningf("Failed to report DeployClusterStart event with ID: %s", event.GetUuid())
	}
	log.V(1).Infof("Reported DeployClusterStart event with ID: %s", event.GetUuid())
	return event.GetUuid()
}

// ReportDeployClusterEnd reports that a cluster deployment has ended.
func ReportDeployClusterEnd(ctx context.Context, id string, err error) {
	event := newEvent()
	event.Uuid = id
	end := &epb.DeployClusterEnd{}
	if err != nil {
		end.Error = err.Error()
	}
	event.Event = &epb.KNEEvent_DeployClusterEnd{DeployClusterEnd: end}
	if err := publishEvent(ctx, event); err != nil {
		log.Warningf("Failed to report DeployClusterEnd event with ID: %s", event.GetUuid())
	}
	log.V(1).Infof("Reported DeployClusterEnd event with ID: %s", event.GetUuid())
	return
}

func toTopologyEvent(topo *tpb.Topology) *epb.Topology {
	t := &epb.Topology{
		LinkCount: int64(len(topo.Links)),
	}
	for _, node := range topo.Nodes {
		t.Nodes = append(t.Nodes, &epb.Node{
			Vendor: node.Vendor,
			Model:  node.Model,
		})
	}
	return t
}

// ReportCreateTopologyStart reports that a topology creation has started.
func ReportCreateTopologyStart(ctx context.Context, t *tpb.Topology) string {
	event := newEvent()
	event.Event = &epb.KNEEvent_CreateTopologyStart{CreateTopologyStart: &epb.CreateTopologyStart{Topology: toTopologyEvent(t)}}
	if err := publishEvent(ctx, event); err != nil {
		log.Warningf("Failed to report CreateTopologyStart event with ID: %s", event.GetUuid())
	}
	log.V(1).Infof("Reported CreateTopologyStart event with ID: %s", event.GetUuid())
	return event.GetUuid()
}

// ReportCreateTopologyEnd reports that a topology creation has ended.
func ReportCreateTopologyEnd(ctx context.Context, id string, err error) {
	event := newEvent()
	event.Uuid = id
	end := &epb.CreateTopologyEnd{}
	if err != nil {
		end.Error = err.Error()
	}
	event.Event = &epb.KNEEvent_CreateTopologyEnd{CreateTopologyEnd: end}
	if err := publishEvent(ctx, event); err != nil {
		log.Warningf("Failed to report CreateTopologyEnd event with ID: %s", event.GetUuid())
	}
	log.V(1).Infof("Reported CreateTopologyEnd event with ID: %s", event.GetUuid())
	return
}
