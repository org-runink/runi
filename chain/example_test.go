// SPDX-License-Identifier: BSD-3-Clause

package chain_test

import (
	"fmt"

	"github.com/org-runink/runi/chain"
)

func Example() {
	var links []chain.Link
	prev := ""
	for _, body := range []string{`{"approve":"order 1"}`, `{"approve":"order 2"}`, `{"approve":"order 3"}`} {
		l := chain.Seal(chain.Bound, prev, []byte(body))
		links = append(links, l)
		prev = l.Hash
	}
	_, err := chain.Verify(chain.Bound, links)
	fmt.Println("intact:", err == nil)

	links[0], links[1] = links[1], links[0]
	_, err = chain.Verify(chain.Bound, links)
	fmt.Println(err)
	// Output:
	// intact: true
	// chain: link 0 is out of order: the link it was sealed after is elsewhere in the chain
}
