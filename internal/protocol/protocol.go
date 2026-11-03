// Package protocol defines the messages between the agent, the server and the web.
// The web's TypeScript types are generated from this file.
package protocol

import (
	"encoding/json"
	"time"
)

type Type string

const (
	// Snapshot carries every object; the web replaces what it had.
	Snapshot Type = "snapshot"
	Added    Type = "added"
	// Modified is only sent when the projection changed.
	Modified Type = "modified"
	Deleted  Type = "deleted"
	Event    Type = "event"
	Check    Type = "check"

	// Sent by the server.
	// Code tells the web which code to show in `ktrek connect <code>`.
	Code         Type = "code"
	Connected    Type = "connected"
	Disconnected Type = "disconnected"
	// Resync asks the agent for a new snapshot, after the web reloads.
	Resync Type = "resync"
)

type Message struct {
	Type    Type          `json:"type"`
	Objects []Object      `json:"objects,omitempty"`
	Object  *Object       `json:"object,omitempty"`
	Event   *ClusterEvent `json:"event,omitempty"`
	Check   *StepCheck    `json:"check,omitempty"`
	Code    string        `json:"code,omitempty"`
}

// Object is what the web needs to draw a node of the graph. Never the spec,
// Secret contents or environment variable values.
type Object struct {
	Kind      string            `json:"kind"`
	Namespace string            `json:"namespace"`
	Name      string            `json:"name"`
	Owners    []string          `json:"owners,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	// Selector is set for Services, so the web can link them to their Pods.
	Selector map[string]string `json:"selector,omitempty"`
	Status   json.RawMessage   `json:"status,omitempty"`
}

// ClusterEvent is a Kubernetes Event, shown in the timeline.
type ClusterEvent struct {
	Object  string    `json:"object"`
	Type    string    `json:"type"`
	Reason  string    `json:"reason"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
}

// StepCheck is the result of the current step's validation.
type StepCheck struct {
	Step   string `json:"step"`
	Passed bool   `json:"passed"`
}
