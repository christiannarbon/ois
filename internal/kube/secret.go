package kube

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
)

// GetSecret returns decoded data (base64 handled), or found=false.
func (k *Client) GetSecret(ctx context.Context, ns, name string) (data map[string]string, found bool, err error) {
	out, found, err := k.GetJSON(ctx, ns, "secret", name)
	if err != nil || !found {
		return nil, false, err
	}
	var s struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, false, fmt.Errorf("reading secret %s/%s: %w", ns, name, err)
	}
	data = make(map[string]string, len(s.Data))
	for key, v := range s.Data {
		b, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return nil, false, fmt.Errorf("reading secret %s/%s key %s: %w", ns, name, key, err)
		}
		data[key] = string(b)
	}
	return data, true, nil
}

// ApplySecret creates or updates an Opaque Secret. The manifest is JSON sent on stdin and
// every value is passed to execx as a secret, so no value reaches argv or a log line.
// labels are merged with app.kubernetes.io/managed-by=ois.
func (k *Client) ApplySecret(ctx context.Context, ns, name string, data map[string]string, labels map[string]string) error {
	secrets := make([]string, 0, len(data))
	for _, v := range data {
		secrets = append(secrets, v)
	}
	sort.Strings(secrets)
	l := map[string]string{}
	for key, v := range labels {
		l[key] = v
	}
	l[managedBy] = "ois"
	return k.applyObject(ctx, map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"type":       "Opaque",
		"metadata":   map[string]any{"name": name, "namespace": ns, "labels": l},
		"stringData": data,
	}, secrets...)
}
