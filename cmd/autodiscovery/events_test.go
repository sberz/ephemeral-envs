package main

import (
	"context"
	"testing"
)

func TestParseMetadataAnnotation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  any
		name  string
	}{
		{
			name:  "json bool",
			value: "true",
			want:  true,
		},
		{
			name:  "json number",
			value: "7",
			want:  float64(7),
		},
		{
			name:  "json string",
			value: `"team-platform"`,
			want:  "team-platform",
		},
		{
			name:  "json object falls back to static string",
			value: `{"owner":"team-platform"}`,
			want:  `{"owner":"team-platform"}`,
		},
		{
			name:  "invalid json falls back to static string",
			value: `team-platform`,
			want:  `team-platform`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := parseMetadataAnnotation(context.Background(), tt.value)
			got, err := p.Value(context.Background())
			if err != nil {
				t.Fatalf("Value() error = %v", err)
			}

			if got != tt.want {
				t.Fatalf("Value() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
