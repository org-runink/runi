// SPDX-License-Identifier: BSD-3-Clause

package salvage_test

import (
	"fmt"

	"github.com/org-runink/runi/salvage"
)

func ExampleDecode() {
	reply := "Sure! Here is the forecast you asked for:\n" +
		"```json\n{\"region\": \"north\", \"units\": 1240}\n```\n" +
		"Let me know if you want the other regions."

	var f struct {
		Region string `json:"region"`
		Units  int    `json:"units"`
	}
	if err := salvage.Decode(reply, &f); err != nil {
		fmt.Println("no forecast in the reply")
		return
	}
	fmt.Println(f.Region, f.Units)
	// Output: north 1240
}
