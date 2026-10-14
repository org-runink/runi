// SPDX-License-Identifier: BSD-3-Clause

package rate

import (
	"errors"
	"fmt"
	"math"
)

// ErrInvalid is wrapped by every constructor error: a parameter outside the
// range its doc names. Match it with errors.Is; the message text is not part
// of the API.
var ErrInvalid = errors.New("rate: invalid parameter")

func invalid(name string, v any, want string) error {
	return fmt.Errorf("%w: %s = %v, want %s", ErrInvalid, name, v, want)
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
