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

package redisreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redisreceiver"

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v7"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver/scraperhelper"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redisreceiver/internal/metadata"
)

// Runs intermittently, fetching info from Redis, creating metrics/datapoints,
// and feeding them to a metricsConsumer.
type redisScraper struct {
	redisSvc *redisSvc
	settings component.ReceiverCreateSettings
	mb       *metadata.MetricsBuilder
	uptime   time.Duration
}

const redisMaxDbs = 16 // Maximum possible number of redis databases

func newRedisScraper(cfg *Config, settings component.ReceiverCreateSettings) (scraperhelper.Scraper, error) {
	opts := &redis.Options{
		Addr:     cfg.Endpoint,
		Password: cfg.Password,
		Network:  cfg.Transport,
	}

	var err error
	if opts.TLSConfig, err = cfg.TLS.LoadTLSConfig(); err != nil {
		return nil, err
	}
	return newRedisScraperWithClient(newRedisClient(opts), settings, cfg)
}

func newRedisScraperWithClient(client client, settings component.ReceiverCreateSettings, cfg *Config) (scraperhelper.Scraper, error) {
	rs := &redisScraper{
		redisSvc: newRedisSvc(client),
		settings: settings,
		mb:       metadata.NewMetricsBuilder(cfg.Metrics),
	}
	return scraperhelper.NewScraper(typeStr, rs.Scrape)
}

// Scrape is called periodically, querying Redis and building Metrics to send to
// the next consumer. First builds 'fixed' metrics (non-keyspace metrics)
// defined at startup time. Then builds 'keyspace' metrics if there are any
// keyspace lines returned by Redis. There should be one keyspace line per
// active Redis database, of which there can be 16.
func (rs *redisScraper) Scrape(context.Context) (pmetric.Metrics, error) {
	inf, err := rs.redisSvc.info()
	if err != nil {
		return pmetric.Metrics{}, err
	}

	now := pcommon.NewTimestampFromTime(time.Now())
	currentUptime, err := inf.getUptimeInSeconds()
	if err != nil {
		return pmetric.Metrics{}, err
	}

	if rs.uptime == time.Duration(0) || rs.uptime > currentUptime {
		rs.mb.Reset(metadata.WithStartTime(pcommon.NewTimestampFromTime(now.AsTime().Add(-currentUptime))))
	}
	rs.uptime = currentUptime

	rs.recordCommonMetrics(now, inf)
	rs.recordKeyspaceMetrics(now, inf)
	rs.recordLatencyStatsMetrics(now, inf)

	return rs.mb.Emit(), nil
}

// recordCommonMetrics records metrics from Redis info key-value pairs.
func (rs *redisScraper) recordCommonMetrics(ts pcommon.Timestamp, inf info) {
	recorders := rs.dataPointRecorders()
	for infoKey, infoVal := range inf {
		recorder, ok := recorders[infoKey]
		if !ok {
			// Skip unregistered metric.
			continue
		}
		switch recordDataPoint := recorder.(type) {
		case func(pcommon.Timestamp, int64):
			val, err := strconv.ParseInt(infoVal, 10, 64)
			if err != nil {
				rs.settings.Logger.Warn("failed to parse info int val", zap.String("key", infoKey),
					zap.String("val", infoVal), zap.Error(err))
			}
			recordDataPoint(ts, val)
		case func(pcommon.Timestamp, float64):
			val, err := strconv.ParseFloat(infoVal, 64)
			if err != nil {
				rs.settings.Logger.Warn("failed to parse info float val", zap.String("key", infoKey),
					zap.String("val", infoVal), zap.Error(err))
			}
			recordDataPoint(ts, val)
		}
	}
}

// recordKeyspaceMetrics records metrics from 'keyspace' Redis info key-value pairs,
// e.g. "db0: keys=1,expires=2,avg_ttl=3".
func (rs *redisScraper) recordKeyspaceMetrics(ts pcommon.Timestamp, inf info) {
	for db := 0; db < redisMaxDbs; db++ {
		key := "db" + strconv.Itoa(db)
		str, ok := inf[key]
		if !ok {
			break
		}
		keyspace, parsingError := parseKeyspaceString(db, str)
		if parsingError != nil {
			rs.settings.Logger.Warn("failed to parse keyspace string", zap.String("key", key),
				zap.String("val", str), zap.Error(parsingError))
			continue
		}
		rs.mb.RecordRedisDbKeysDataPoint(ts, int64(keyspace.keys), keyspace.db)
		rs.mb.RecordRedisDbExpiresDataPoint(ts, int64(keyspace.expires), keyspace.db)
		rs.mb.RecordRedisDbAvgTTLDataPoint(ts, int64(keyspace.avgTTL), keyspace.db)
	}
}

// recordLatencyStatsMetrics records metrics from 'LatencyStatsMetrics' Redis info key-value pairs,
// e.g. "latency_percentiles_usec_info:p50=10.123,p99=110.234,p99.9=120.234".
func (rs *redisScraper) recordLatencyStatsMetrics(ts pcommon.Timestamp, inf info) {
	keyPrefix := "latency_percentiles_usec_"
	for infoKey, infoVal := range inf {
		if (!strings.HasPrefix(infoKey, keyPrefix)) || len(infoKey) <= len(keyPrefix) {
			continue
		}
		command := infoKey[len(keyPrefix):len(infoKey)]
		latencystats, parsingError := parseLatencystatsString(command, infoVal)
		if parsingError != nil {
			rs.settings.Logger.Warn("failed to parse latency stats string", zap.String("command", command),
				zap.String("latencystats", infoVal), zap.Error(parsingError))
			continue
		}
		for percentile, latency := range latencystats.stats {
			switch percentile {
			case "50":
				rs.mb.RecordRedisLatencystatP50DataPoint(ts, float64(latency), command)
			case "90":
				rs.mb.RecordRedisLatencystatP90DataPoint(ts, float64(latency), command)
			case "99":
				rs.mb.RecordRedisLatencystatP99DataPoint(ts, float64(latency), command)
			case "99.9":
				rs.mb.RecordRedisLatencystatP999DataPoint(ts, float64(latency), command)
			case "100":
				rs.mb.RecordRedisLatencystatP100DataPoint(ts, float64(latency), command)
			}
		}

	}
}
