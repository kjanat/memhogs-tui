package main

import (
	"testing"
	"time"
)

func TestResolveInterval(t *testing.T) {
	tests := []struct {
		name     string
		flag     int
		explicit bool
		args     []string
		watch    string
		set      bool
		want     time.Duration
		bad      bool
	}{
		{name: "default", flag: 3, want: 3 * time.Second},
		{name: "watch", flag: 3, watch: "7", set: true, want: 7 * time.Second},
		{name: "positional beats watch", flag: 3, args: []string{"5"}, watch: "7", set: true, want: 5 * time.Second},
		{name: "flag beats watch and positional", flag: 2, explicit: true, args: []string{"5"}, watch: "7", set: true, want: 2 * time.Second},
		{name: "explicit default beats watch", flag: 3, explicit: true, watch: "7", set: true, want: 3 * time.Second},
		{name: "irrelevant invalid watch", flag: 2, explicit: true, watch: "bad", set: true, want: 2 * time.Second},
		{name: "zero", flag: 0, explicit: true, bad: true},
		{name: "negative", flag: -1, explicit: true, bad: true},
		{name: "bad positional", flag: 3, args: []string{"bad"}, bad: true},
		{name: "extra arguments", flag: 3, args: []string{"3", "4"}, bad: true},
		{name: "bad watch", flag: 3, watch: "bad", set: true, bad: true},
		{name: "empty watch", flag: 3, set: true, bad: true},
		{name: "zero watch", flag: 3, watch: "0", set: true, bad: true},
		{name: "negative positional", flag: 3, args: []string{"-2"}, bad: true},
		{name: "fraction", flag: 3, args: []string{"0.5"}, bad: true},
		{name: "overflow", flag: 3, args: []string{"9223372037"}, bad: true},
		{name: "maximum seconds", flag: 3, args: []string{"9223372036"}, want: 9223372036 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveInterval(tt.flag, tt.explicit, tt.args, tt.watch, tt.set)
			if (err != nil) != tt.bad || got != tt.want {
				t.Fatalf("got %v, %v; want %v, error=%v", got, err, tt.want, tt.bad)
			}
		})
	}
}
