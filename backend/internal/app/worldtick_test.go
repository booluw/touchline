package app

import "testing"

func TestTickDay(t *testing.T) {
	cases := []struct {
		payload string
		want    int64
		ok      bool
	}{
		{`{"granularity":"daily","day":14}`, 14, true},
		{`{"granularity":"daily","day":0}`, 0, true},
		{`{"granularity":"daily"}`, 0, false}, // pre-IM23 emission
		{`not json`, 0, false},
	}
	for _, c := range cases {
		got, ok := tickDay([]byte(c.payload))
		if got != c.want || ok != c.ok {
			t.Errorf("tickDay(%s) = (%d, %v), want (%d, %v)", c.payload, got, ok, c.want, c.ok)
		}
	}
}
