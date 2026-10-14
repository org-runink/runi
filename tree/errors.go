// SPDX-License-Identifier: BSD-3-Clause

package tree

import (
	"errors"
	"fmt"
	"math"
)

// ErrInvalid is wrapped by every error this package returns: an argument the
// tree cannot accept in its current state. The more specific sentinels below
// wrap it too, so errors.Is(err, ErrInvalid) holds for all of them. Match
// with errors.Is; the message text is not part of the API.
var ErrInvalid = errors.New("tree: invalid argument")

var (
	// ErrUnknown: a node named as the subject or the new parent is not in
	// the tree.
	ErrUnknown = fmt.Errorf("%w: unknown node", ErrInvalid)
	// ErrExists: Add or AddRoot with a key already in the tree.
	ErrExists = fmt.Errorf("%w: node exists", ErrInvalid)
	// ErrCycle: Move would place a node under itself or one of its
	// descendants.
	ErrCycle = fmt.Errorf("%w: node would become its own ancestor", ErrInvalid)
	// ErrHasChildren: Remove of a node that is not a leaf.
	ErrHasChildren = fmt.Errorf("%w: node has children", ErrInvalid)
)

// maxValue bounds |v| in Set. Sums of up to 2^60 values this large, and the
// intermediate terms compensated summation forms from them, stay below
// math.MaxFloat64, so no sum can overflow to ±Inf and poison its ancestors.
const maxValue = 0x1p960

func unknown[K comparable](id K) error { return fmt.Errorf("%w: %v", ErrUnknown, id) }

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
