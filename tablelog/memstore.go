// SPDX-License-Identifier: BSD-3-Clause

package tablelog

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

// MemStore is an in-memory [Store]: the reference implementation, and enough to
// run a table in a test without any infrastructure.
//
// It is NOT a toy. It implements the one guarantee the table's correctness
// rests on — PutIfAbsent is an atomic create — and it is safe for concurrent
// use, so the optimistic-concurrency paths can be exercised honestly rather
// than simulated. What it is not is durable: everything is lost when it goes
// out of scope.
//
// Use it to try the package, to test code that writes tables, and as the
// shortest correct example of what a real Store must do.
type MemStore struct {
	mu      sync.RWMutex
	objects map[string][]byte

	// FailPutIfAbsent, when set, is consulted before every atomic create. A
	// non-nil return is returned to the caller instead of performing it, which
	// is how a test injects a crash between the data write and the log write.
	FailPutIfAbsent func(key string) error
}

// NewMemStore returns an empty in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{objects: make(map[string][]byte)}
}

// Put writes key, overwriting any existing object.
func (m *MemStore) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = buf.Bytes()
	return nil
}

// PutIfAbsent creates key only if it does not exist, and returns ErrExists if
// it does. This is the atomic create the table's correctness depends on: two
// writers racing for the same log version, exactly one winning.
func (m *MemStore) PutIfAbsent(ctx context.Context, key string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.FailPutIfAbsent != nil {
		if err := m.FailPutIfAbsent(key); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.objects[key]; ok {
		return fmt.Errorf("tablelog: %q: %w", key, ErrExists)
	}
	m.objects[key] = append([]byte(nil), data...)
	return nil
}

// Get returns a reader over key.
func (m *MemStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("tablelog: %q: not found", key)
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), b...))), nil
}

// List returns the keys under prefix, sorted. The order matters: the log is
// read by version, and an unsorted List would replay commits out of order.
func (m *MemStore) List(ctx context.Context, prefix string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.objects))
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Delete removes key. Deleting something absent is not an error: Vacuum may
// race another Vacuum, and both should succeed.
func (m *MemStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

// Len reports how many objects are stored, for tests that assert a Vacuum
// actually removed something.
func (m *MemStore) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.objects)
}

var _ Store = (*MemStore)(nil)

// Overwrite replaces an object unconditionally, which no Store method can do.
// It exists so a test can corrupt a specific object and check that the table
// reports it rather than reading past it; production code must go through
// PutIfAbsent so a lost race is still a lost race.
func (m *MemStore) Overwrite(key string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = append([]byte(nil), data...)
}
