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
	"fmt"
	"strconv"
	"strings"
)

// Holds fields returned by the latencyStats section of the INFO command: e.g.
// "latency_percentiles_usec_info:p50=10.123,p99=110.234,p99.9=120.234"
type latencystats struct {
	command string
	stats   map[string]float64
}

// Turns a latencyStats value (the part after the colon
// e.g. "p50=10.123,p99=110.234,p99.9=120.234") into a latencystats struct
func parseLatencystatsString(command string, str string) (*latencystats, error) {
	pairs := strings.Split(str, ",")
	las := latencystats{command: command, stats: make(map[string]float64)}
	for _, pairStr := range pairs {
		pair := strings.Split(pairStr, "=")
		if len(pair) != 2 {
			return nil, fmt.Errorf(
				"unexpected latencystats pair '%s'",
				pairStr,
			)
		}
		if !strings.HasPrefix(pair[0], "p") || len(pair[0]) < 2 { // should be pxx
			return nil, fmt.Errorf(
				"unexpected latencystats key '%s'", pair[0],
			)

		}
		key := pair[0][1:]
		_, err := strconv.ParseFloat(key, 64)
		if err != nil {
			return nil, fmt.Errorf(
				"unexpected latencystats key '%s'", pair[0],
			)
		}
		if _, ok := (&las).stats[key]; ok {
			return nil, fmt.Errorf(
				"multiple stats in one command '%s' for the same percentile value '%s'",
				command, key,
			)
		}
		val, err := strconv.ParseFloat(pair[1], 64)
		if err != nil {
			return nil, err
		}
		(&las).stats[key] = val
	}
	return &las, nil
}
