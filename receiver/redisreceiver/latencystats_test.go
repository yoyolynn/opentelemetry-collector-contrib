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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseLatencyStats(t *testing.T) {
	latencystats, err := parseLatencystatsString("info", "p50=10.123,p99=110.234,p99.9=120.234")
	require.Nil(t, err)
	require.Equal(t, "info", latencystats.command)
	require.Equal(t, 10.123, latencystats.stats["50"])
	require.Equal(t, 110.234, latencystats.stats["99"])
	require.Equal(t, 120.234, latencystats.stats["99.9"])
}

func TestParseMalformedLatencyStats(t *testing.T) {
	tests := []struct{ name, info string }{
		{"missing value", "p50=10.123,p99=110.234,p99.9="},
		{"missing equals", "p50=10.123,p99=110.234,p99.9"},
		{"unexpected key", "a50=10.123,p99=110.234,p99.9=120.234"},
		{"invalid percentile", "pa10=10.123,p99=110.234,p99.9=120.234"},
		//	{"unexpected percentile", "p10=10.123,p99=110.234,p99.9=120.234"},
		{"no usable data", "foo"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseLatencystatsString("info", test.info)
			require.NotNil(t, err)
		})
	}
}
