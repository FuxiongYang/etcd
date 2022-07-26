// Copyright 2021 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package etcdhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.etcd.io/etcd/etcdserver"
	stats "go.etcd.io/etcd/etcdserver/api/v2stats"
	pb "go.etcd.io/etcd/etcdserver/etcdserverpb"
	"go.etcd.io/etcd/pkg/testutil"
	"go.etcd.io/etcd/pkg/types"
	"go.etcd.io/etcd/raft"
)

type fakeStats struct{}

func (s *fakeStats) SelfStats() []byte   { return nil }
func (s *fakeStats) LeaderStats() []byte { return nil }
func (s *fakeStats) StoreStats() []byte  { return nil }

type fakeServerV2 struct {
	fakeServer
	stats.Stats
	health string
}

func (s *fakeServerV2) Leader() types.ID {
	if s.health == "true" {
		return 1
	}
	return types.ID(raft.None)
}
func (s *fakeServerV2) Do(ctx context.Context, r pb.Request) (etcdserver.Response, error) {
	if s.health == "true" {
		return etcdserver.Response{}, nil
	}
	return etcdserver.Response{}, fmt.Errorf("fail health check")
}
func (s *fakeServerV2) ClientCertAuthEnabled() bool { return false }

func TestHealthHandler(t *testing.T) {
	// define the input and expected output
	// input: alarms, and healthCheckURL
	tests := []struct {
		alarms         []*pb.AlarmMember
		healthCheckURL string
		statusCode     int
		health         string
	}{
		{
			[]*pb.AlarmMember{},
			"/health",
			http.StatusOK,
			"true",
		},
		{
			[]*pb.AlarmMember{{MemberID: uint64(0), Alarm: pb.AlarmType_NOSPACE}},
			"/health",
			http.StatusServiceUnavailable,
			"false",
		},
		{
			[]*pb.AlarmMember{{MemberID: uint64(0), Alarm: pb.AlarmType_NOSPACE}},
			"/health?exclude=NOSPACE",
			http.StatusOK,
			"true",
		},
		{
			[]*pb.AlarmMember{},
			"/health?exclude=NOSPACE",
			http.StatusOK,
			"true",
		},
		{
			[]*pb.AlarmMember{{MemberID: uint64(1), Alarm: pb.AlarmType_NOSPACE}, {MemberID: uint64(2), Alarm: pb.AlarmType_NOSPACE}, {MemberID: uint64(3), Alarm: pb.AlarmType_NOSPACE}},
			"/health?exclude=NOSPACE",
			http.StatusOK,
			"true",
		},
		{
			[]*pb.AlarmMember{{MemberID: uint64(0), Alarm: pb.AlarmType_NOSPACE}, {MemberID: uint64(1), Alarm: pb.AlarmType_CORRUPT}},
			"/health?exclude=NOSPACE",
			http.StatusServiceUnavailable,
			"false",
		},
		{
			[]*pb.AlarmMember{{MemberID: uint64(0), Alarm: pb.AlarmType_NOSPACE}, {MemberID: uint64(1), Alarm: pb.AlarmType_CORRUPT}},
			"/health?exclude=NOSPACE&exclude=CORRUPT",
			http.StatusOK,
			"true",
		},
	}

	for i, tt := range tests {
		func() {
			mux := http.NewServeMux()
			HandleMetricsHealth(mux, &fakeServerV2{
				fakeServer: fakeServer{alarms: tt.alarms},
				Stats:      &fakeStats{},
				health:     tt.health,
			})
			ts := httptest.NewServer(mux)
			defer ts.Close()

			res, err := ts.Client().Do(&http.Request{Method: http.MethodGet, URL: testutil.MustNewURL(t, ts.URL+tt.healthCheckURL)})
			if err != nil {
				t.Errorf("fail serve http request %s %v in test case #%d", tt.healthCheckURL, err, i+1)
			}
			if res == nil {
				t.Errorf("got nil http response with http request %s in test case #%d", tt.healthCheckURL, i+1)
				return
			}
			if res.StatusCode != tt.statusCode {
				t.Errorf("want statusCode %d but got %d in test case #%d", tt.statusCode, res.StatusCode, i+1)
			}
			health, err := parseHealthOutput(res.Body)
			if err != nil {
				t.Errorf("fail parse health check output %v", err)
			}
			if health.Health != tt.health {
				t.Errorf("want health %s but got %s", tt.health, health.Health)
			}
		}()
	}
}

func parseHealthOutput(body io.Reader) (Health, error) {
	obj := Health{}
	d, derr := ioutil.ReadAll(body)
	if derr != nil {
		return obj, derr
	}
	if err := json.Unmarshal(d, &obj); err != nil {
		return obj, err
	}
	return obj, nil
}
