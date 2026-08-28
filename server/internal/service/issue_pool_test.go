package service

import "testing"

func TestPoolIssueMayDispatch(t *testing.T) {
	tests := []struct {
		name     string
		metadata string
		want     bool
	}{
		{"no owner", `{}`, true},
		{"agent owner", `{"waiting_on":"agent:worker"}`, true},
		{"ceo owner", `{"waiting_on":"ceo"}`, false},
		{"external owner", `{"waiting_on":"external"}`, false},
		{"invalid metadata", `{`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := poolIssueMayDispatch([]byte(tt.metadata)); got != tt.want {
				t.Fatalf("poolIssueMayDispatch(%s) = %v, want %v", tt.metadata, got, tt.want)
			}
		})
	}
}
