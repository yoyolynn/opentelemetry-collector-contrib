// Copyright 2020, OpenTelemetry Authors
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

package redisreceiver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/configtls"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redisreceiver/internal/metadata"
)

func TestRedisRunnable(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	settings := componenttest.NewNopReceiverCreateSettings()
	settings.Logger = logger
	cfg := createDefaultConfig().(*Config)
	rs := &redisScraper{mb: metadata.NewMetricsBuilder(cfg.Metrics)}
	runner, err := newRedisScraperWithClient(newFakeClient(), settings, cfg)
	require.NoError(t, err)
	md, err := runner.Scrape(context.Background())
	require.NoError(t, err)
	// + 6 because there are two keyspace entries each of which has three metrics
	// + 15 because there are five command latency entries each of which has three percentile stats
	assert.Equal(t, len(rs.dataPointRecorders())+6+15, md.DataPointCount())
	rm := md.ResourceMetrics().At(0)
	ilm := rm.ScopeMetrics().At(0)
	il := ilm.Scope()
	assert.Equal(t, "otelcol/redisreceiver", il.Name())
	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		rmi := rms.At(i)
		ilms := rmi.ScopeMetrics()
		for j := 0; j < ilms.Len(); j++ {
			ilm := ilms.At(j)
			ms := ilm.Metrics()
			for k := 0; k < ms.Len(); k++ {
				m := ms.At(k)
				if m.Name() == "redis.latencystat.p50" {
					for idx := 0; idx < m.Gauge().DataPoints().Len(); idx++ {
						if m.Gauge().DataPoints().At(idx).Attributes().AsRaw()["command"] == "dbsize" {
							assert.Equal(t, 30.345, m.Gauge().DataPoints().At(idx).DoubleVal())
						}
					}
				}
			}
		}
	}
}

func TestNewReceiver_invalid_auth_error(t *testing.T) {
	c := createDefaultConfig().(*Config)
	c.TLS = configtls.TLSClientSetting{
		TLSSetting: configtls.TLSSetting{
			CAFile: "/invalid",
		},
	}
	r, err := createMetricsReceiver(context.Background(), componenttest.NewNopReceiverCreateSettings(), c, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load TLS config")
	assert.Nil(t, r)
}
