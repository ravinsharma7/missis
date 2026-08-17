# Bitemporal scalar winner rule — DRAFT subspec

> **Transient:** this is a working draft, not an authoritative contract.
> It will be merged into `missues-issue-specification.v2.md` and then deleted.
> See AGENTS.md "Document authority".

**Status:** DRAFT proposal — decisions below were resolved 2026-08-17 and are
recorded for review. This is a working subspec, not permanent. When the rule
is accepted it must be merged into
`missues-issue-specification.v2.md` (section 10, temporal ledger) and the
requirements registry updated; this file is then removed.

**Tracking:** Missis ticket #42
**Source:** `reports/hardening-report1.md` section 8

## 1. Problem

The current projector filters events by `effective_at <= V` and
`recorded_at <= K`, then folds them sorted by `(stream_sequence, recorded_at,
event_id)`. The main spec says "fold events where effective_at <= t" without
defining the fold order or the winner rule. Two correct-looking
implementations can therefore produce different state. This subspec pins the
rule before it is implemented and tested.

## 2. Terms

```text
recorded_at   when Missis accepted the event (transaction time)
effective_at  when the assertion became true in the represented domain (valid time)
K             known time: only events with recorded_at <= K are visible
V             valid time: only events with effective_at <= V apply
stream_sequence  deterministic acceptance order within a stream
```

## 3. Candidate set and winner rule (decided)

```text
Candidates(x, V, K) =
    { e : target(e) = x
          and recorded_at(e) <= K
          and effective_at(e) <= V
          and not superseded_as_of(e, K) }

Winner(x, V, K) = argmax over Candidates of
    (effective_at, recorded_at, stream_sequence, event_id)
```

Interpretation: each assertion applies from its `effective_at` until a
candidate with a strictly greater `(effective_at, recorded_at,
stream_sequence, event_id)` tuple wins. This is the historical-correction
model: a backdated assertion changes the interval it names, not the whole
current state.

## 4. Boundary policy (decided)

Bounds are inclusive: `recorded_at <= K` and `effective_at <= V`. An event
whose effective time equals the query valid time is visible.

## 5. Retraction (decided)

A retraction at effective time `T` means:

```text
no value for the target from T onward
until a later assertion wins
```

It does not mean the value never existed. The retraction is invisible at any
known time before the retraction was recorded.

**Decision (2026-08-17):** interval hole (nil). A tombstone was rejected
because it can be mistaken for a real value by consumers.

Projection fields (decided):

```text
CurrentFrom = the event that established the value visible at (V, K)
RetractedBy = the retraction event that opened the current hole, if any
```

The current-state projection keeps these as "winning event at (V, K)"; full
interval rendering belongs to a future timeline view, not to the current
projection.

## 6. Supersession (decided)

`Supersedes(new, old)` must target the same canonical target and the same
operation family. A superseding event hides the superseded event in any
projection where the superseder is known:

```text
superseded_as_of(e, K)  ⇔  exists superseder s: supersedes(s, e) and recorded_at(s) <= K
```

**Decision (2026-08-17):** Option A — the superseded event is void as soon as
the superseding event is known, even when the superseder is not yet effective.
Rejected Option B (superseded value stands until the superseder becomes
effective) as inconsistent with "old hidden as of K".

## 7. Timestamp precision (decided)

All comparisons use absolute instants (timezone-normalized), so precision does
not change ordering. Precision matters for canonical bytes and cross-language
reproducibility.

**Decision (2026-08-17):** 9-digit nanoseconds.

```text
canonical form: UTC, fixed fractional precision, Z suffix
precision: 9-digit nanoseconds, trailing zeros retained
inputs are normalized at append so stored bytes are deterministic
```

Microsecond truncation was rejected: it discards precision and offers no
simplification worth the loss. The contract is recorded on ticket #45.

This must be pinned before the canonical event encoding (#45) is finalized,
because it changes hash bytes. The same precision applies to the truth-table
tests below so expected outputs are exact.

## 8. Truth table (10 worked examples)

Times are listed as `recorded / effective`. Queries are `(V, K)`.

| # | Case | Events | Query (V, K) | Expected |
|---|---|---|---|---|
| 1 | Normal update | e1 `10:00 / 10:00` open; e2 `12:00 / 12:00` done | `(13:00, 14:00)` | done |
| 2 | Backdated update | e1 `10:00 / 10:00` open; e2 `12:00 / 12:00` done; e3 `13:00 / 11:00` doing | `(11:30, 14:00)` | doing (timeline: open [10,11), doing [11,12), done [12,∞)) |
| 3 | Future update | e1 `Mon / Mon` open; e2 `Mon / Fri` done | `(Wed, Thu)` / `(Sat, Thu)` | open / done |
| 4 | Same effective time | e1 `10:00 / 12:00` open; e2 `11:00 / 12:00` done | `(12:00, 12:00)` | done (later recorded wins) |
| 5 | Retraction | e1 `10:00 / 09:00` A; e2 `14:00 / 11:00` retract | `(12:00, 15:00)` / `(12:00, 13:00)` | no value / A |
| 6 | Backdated retraction | e1 `10:00 / 09:00` A; e2 `14:00 / 10:30` retract | `(10:45, 15:00)` / `(10:45, 13:00)` | no value / A |
| 7 | Supersession | e1 `10:00 / 10:00` A; e2 `12:00 / 11:00` B supersedes e1 | `(10:30, 11:30)` / `(10:30, 13:00)` / `(13:00, 13:00)` | A / no value (decision 6) / B |
| 8 | Out-of-order import | e1 `09:00 / 12:00` done; e2 `08:00 / 10:00` open (imported in either order) | `(11:00, 12:00)` / `(13:00, 12:00)` | open / done, independent of insertion order |
| 9 | Backdated recursive move | move of part to parent X `13:00 / 11:00` | `(10:30, 14:00)` / `(11:30, 14:00)` | old location / X |
| 10 | Backdated link retraction | assert `10:00 / 09:00`; retract `13:00 / 10:30` | `(11:00, 14:00)` / `(11:00, 12:00)` | link inactive / link active |

## 9. Implementation approach (once accepted)

- Build per-target timelines instead of one global event sort.
- Filter by known time, group by canonical target (or relation identity),
  apply supersession/retraction visibility, evaluate at the requested valid
  time, then assemble hierarchy and links.
- The ten cases above become table-driven projection tests before the
  projector is changed.

## 10. Merge plan

```text
accept rule and resolve decisions 6 and 7
    -> add section to missues-issue-specification.v2.md
    -> update requirements registry
    -> remove this subspec file
```
