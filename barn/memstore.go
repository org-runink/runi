// SPDX-License-Identifier: BSD-3-Clause

package barn

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
	objects map[string][]byte // created on first write, so the zero value works

	// failPutIfAbsent, when set, is consulted before every atomic create. A
	// non-nil return is returned to the caller instead of performing it, which
	// is how a test injects a crash between the data write and the log write.
	// It is read under mu and called after mu is released, so a hook may use
	// the store itself, or replace itself, without deadlocking.
	failPutIfAbsent func(key string) error
}

// NewMemStore returns an empty in-memory store. It is equivalent to new(MemStore):
// the zero value is an empty store ready to use.
func NewMemStore() *MemStore {
	return &MemStore{objects: make(map[string][]byte)}
}

// set stores a copy of data under key. The caller holds mu for writing.
func (m *MemStore) set(key string, data []byte) {
	if m.objects == nil {
		m.objects = make(map[string][]byte)
	}
	m.objects[key] = append([]byte(nil), data...)
}

// Put writes key, overwriting any existing object.
func (m *MemStore) Put(ctx context.Context, key string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.set(key, data)
	return nil
}

// PutIfAbsent creates key only if it does not exist, and returns an error
// wrapping ErrExists if it does. This is the atomic create the table's
// correctness depends on: two writers racing for the same log version, exactly
// one winning.
func (m *MemStore) PutIfAbsent(ctx context.Context, key string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.RLock()
	fail := m.failPutIfAbsent
	m.mu.RUnlock()
	if fail != nil {
		if err := fail(key); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.objects[key]; ok {
		return fmt.Errorf("barn: %q: %w", key, ErrExists)
	}
	m.set(key, data)
	return nil
}

// Get returns a reader over key, or an error wrapping ErrNotFound if there is
// no such object.
func (m *MemStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("barn: %q: %w", key, ErrNotFound)
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), b...))), nil
}

// List returns the keys under prefix, sorted. The Store contract allows any
// order; sorting makes MemStore's answers deterministic.
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

var _ Store = (*MemStore)(nil)

// numObjects reports how many objects are stored, for tests that assert a
// Vacuum actually removed something.
func (m *MemStore) numObjects() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.objects)
}

// overwrite replaces an object unconditionally. It exists so a test can corrupt
// a specific object and check that the table reports it rather than reading
// past it; Put could do the same, but going through this says what the test
// means.
func (m *MemStore) overwrite(key string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.set(key, data)
}

// setFailPutIfAbsent installs (or, with nil, removes) the fault-injection hook
// PutIfAbsent consults. It takes the lock, so a test may change the hook while
// other goroutines are writing.
func (m *MemStore) setFailPutIfAbsent(f func(key string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failPutIfAbsent = f
}
