// SPDX-License-Identifier: BSD-3-Clause

// Package chain makes a sequence of records tamper-evident: each record is
// sealed with a hash that covers the record before it, so changing, moving or
// swapping one breaks the chain at that point, and Verify says where and how.
//
// It is for logs that must not be quietly rewritten: decisions taken on
// someone's behalf, approvals, payments released. The records are yours. You
// give chain the bytes that stand for a record (your canonical encoding), and
// it keeps the links:
//
//	l1 := chain.Seal(chain.Bound, "", body1)
//	l2 := chain.Seal(chain.Bound, l1.Hash, body2)
//	head, err := chain.Verify(chain.Bound, []chain.Link{l1, l2})
//
// Verify walks the links in order and stops at the first break, reporting
// what kind it is:
//
//   - Altered: a link's body no longer matches its hash. It was edited after
//     it was sealed.
//   - Reordered: a link names, as the one before it, a link that is in the
//     chain but somewhere else. Records were moved.
//   - Replaced: a link names, as the one before it, a hash that is not in the
//     chain at all. The record it was sealed after was swapped out or
//     removed.
//
// # Choosing a hasher
//
// Bound hashes the previous link's hash together with the body, with a domain
// prefix, so the link is part of what is sealed. Use it for new chains.
// BodyOnly hashes the body alone; use it only when your body already carries
// the previous hash (for example as a prev_hash field in the encoded record),
// which is how many existing logs are built. With BodyOnly and a body that
// does not carry it, the hash does not bind the order, and Verify can only
// catch a reorder through the Prev fields, which an editor can rewrite.
//
// # What it does not do
//
// It is tamper-EVIDENT, not tamper-proof, and nothing is signed. Whoever can
// write the log can rewrite the whole chain consistently from any point on.
// Removing the NEWEST records leaves a shorter chain that still verifies: keep
// each new head hash somewhere the writer cannot change (another system, a
// published digest) and compare against it.
//
// It does not encode your records. Two encodings of the same record that
// differ by one byte are different records to chain, so use one canonical
// encoding, the same one for sealing and verifying.
//
// It keeps nothing: storage, sequence numbers and concurrency are yours.
package chain
