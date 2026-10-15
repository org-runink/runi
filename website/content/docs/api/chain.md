---
title: "runi/chain"
weight: 11
---

```go
import "github.com/org-runink/runi/chain"
```

Package chain makes a sequence of records tamper-evident: each record is
sealed with a hash that covers the record before it, so changing, moving or
swapping one breaks the chain at that point, and Verify says where and how.

It is for logs that must not be quietly rewritten: decisions taken on
someone's behalf, approvals, payments released. The records are yours. You
give chain the bytes that stand for a record (your canonical encoding), and
it keeps the links:

l1 := chain.Seal(chain.Bound, "", body1)
l2 := chain.Seal(chain.Bound, l1.Hash, body2)
head, err := chain.Verify(chain.Bound, []chain.Link{l1, l2})

Verify walks the links in order and stops at the first break, reporting
what kind it is:

  - Altered: a link's body no longer matches its hash. It was edited after
    it was sealed.
  - Reordered: a link names, as the one before it, a link that is in the
    chain but somewhere else. Records were moved.
  - Replaced: a link names, as the one before it, a hash that is not in the
    chain at all. The record it was sealed after was swapped out or
    removed.

# Choosing a hasher

Bound hashes the previous link's hash together with the body, with a domain
prefix, so the link is part of what is sealed. Use it for new chains.
BodyOnly hashes the body alone; use it only when your body already carries
the previous hash (for example as a prev_hash field in the encoded record),
which is how many existing logs are built. With BodyOnly and a body that
does not carry it, the hash does not bind the order, and Verify can only
catch a reorder through the Prev fields, which an editor can rewrite.

# What it does not do

It is tamper-EVIDENT, not tamper-proof, and nothing is signed. Whoever can
write the log can rewrite the whole chain consistently from any point on.
Removing the NEWEST records leaves a shorter chain that still verifies: keep
each new head hash somewhere the writer cannot change (another system, a
published digest) and compare against it.

It does not encode your records. Two encodings of the same record that
differ by one byte are different records to chain, so use one canonical
encoding, the same one for sealing and verifying.

It keeps nothing: storage, sequence numbers and concurrency are yours.

## Index

- `type` [Break](#break)
- `type` [Fault](#fault)
- `type` [Hasher](#hasher)
- `type` [Link](#link)
- `func` [BodyOnly](#bodyonly)
- `func` [Bound](#bound)
- `method` [Break.Error](#breakerror)
- `method` [Fault.String](#faultstring)
- `func` [Seal](#seal)
- `func` [Verify](#verify)
- `func` [VerifyFrom](#verifyfrom)

### Break

```go
type Break struct
```

Break is Verify's error: the first link that does not hold, counted from 0,
and how.

<small>Defined in <code>chain/chain.go</code>.</small>

### Fault

```go
type Fault int
```

Fault is how a chain broke.

<small>Defined in <code>chain/chain.go</code>.</small>

### Hasher

```go
type Hasher func(prev string, body []byte) string
```

A Hasher computes a link's hash from the previous hash and the body.

<small>Defined in <code>chain/chain.go</code>.</small>

### Link

```go
type Link struct
```

Link is one sealed record: the caller's bytes, the hash of the link before
it ("" for the first), and its own hash.

<small>Defined in <code>chain/chain.go</code>.</small>

### BodyOnly

```go
func BodyOnly(_ string, body []byte) string
```

BodyOnly is hex(sha256(body)). Use it only when body already carries the
previous hash; see the package documentation.

<small>Defined in <code>chain/chain.go</code>.</small>

### Bound

```go
func Bound(prev string, body []byte) string
```

Bound is hex(sha256(domain, prev, 0x00, body)): the previous hash is part of
what is sealed. Use it for new chains.

<small>Defined in <code>chain/chain.go</code>.</small>

### Break.Error

```go
func (b *Break) Error() string
```

<small>Defined in <code>chain/chain.go</code>.</small>

### Fault.String

```go
func (f Fault) String() string
```

<small>Defined in <code>chain/chain.go</code>.</small>

### Seal

```go
func Seal(h Hasher, prev string, body []byte) Link
```

Seal makes the link that follows prev.

<small>Defined in <code>chain/chain.go</code>.</small>

### Verify

```go
func Verify(h Hasher, links []Link) (string, error)
```

Verify checks a whole chain, whose first link must follow "". It returns
the head hash ("" for an empty chain) or a *Break at the first link that
does not hold.

<small>Defined in <code>chain/chain.go</code>.</small>

### VerifyFrom

```go
func VerifyFrom(h Hasher, root string, links []Link) (string, error)
```

VerifyFrom checks links that continue a chain whose last known hash is
root: a segment, or a chain checked against a head kept elsewhere.

<small>Defined in <code>chain/chain.go</code>.</small>
