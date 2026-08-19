# Schema declarations — separating data from meaning

**Status:** working subspec draft, rev 2 (2026-08-19). Transient, not merged,
not normative. Merge into the main spec only after hardening (see
`## Merge checklist`); the repo convention is to delete the subspec file once
merged.

**Proposed insertion (deferred):** `specs/missues-issue-specification.v2.md`,
new subsection `## 12.11 Declared value kinds and schema parts`, immediately
after `## 12.10 Example ontology manifest` and before
`## 13. Deterministic ontology and hook cycle`.

**Tracked by:** ticket #27 (Implement runtime ontology loading and
enforcement); design notes recorded in `.missis-store` at `#27/approach` and
`#43/separator-strategy`.

**Rev 2 changes from rev 1 (audit):**

- Wildcard patterns such as `schema/evidence/*` were removed: `*` is not a
  legal path segment (spec 7.4, enforced by `ValidatePathSegments`), so those
  patterns could not exist as parts. Subtree inheritance replaces them:
  a declaration at a key prefix applies to that key and its descendants.
- Declarations live on scope entities (project/group), never inside ticket
  content. A ticket declaring its own meaning would self-govern and pollute
  the open-world content namespace.
- Mechanism status is now explicit: what already exists in the model versus
  what this subsection newly proposes.
- A decision log records each design choice and its rationale.

**Rev 3 changes (spike outcome, 2026-08-19):**

- Type-qualified declarations use the reserved `schema/type/<ticket-type>/<key-prefix>`
  subtree. This removes the ambiguity between a ticket type name and an
  ordinary key prefix.
- Composite kinds are restricted to base kinds in v1 (`list[K]`, `map[K:V]`
  where `K`/`V` are base kinds; no nesting). Wording corrected from
  `list[str]` to `list[text]`: the vocabulary is `text`, not `str`.
- A final deterministic tie-break (canonical declaration order) is added
  after the bitemporal winner rule, so two declarations that tie on scope and
  time still resolve identically in every session.
- Global defaults are declarations with prefix `status`/`priority` and
  participate in the same subtree matching as any other declaration.
- Spike implementation: `internal/schema` implements this core with 14
  passing tests mapped to the acceptance criteria (see 12.11.12).

**Rev 4 changes (2026-08-19, review feedback):**

- All inference is removed, not demoted: there is no fallback and no
  built-in global defaults. `inferValueKind` is deleted, not kept as a
  named fallback, because a shared guessing heuristic invites divergent
  re-implementations and code drift.
- Every write carries an explicit `ValueKind` (declared, or supplied by the
  writer). Resolution is exactly two steps: declared kind, then stored kind.
  A write without an explicit kind is rejected even for undeclared keys.
- Enforcement and imports are all-or-nothing: a multi-part import validates
  every proposed part before the batch is appended; any violation rejects
  the whole batch, never a partial success.

**Rev 5 changes (2026-08-19, implementation):**

- Element-level enforcement is live for list writes: a declared `list[K]`
  requires element-level writes (`--add`), and `list[ref]` elements must
  resolve to known references at write time. `map[K:V]` remains shape-only
  until a structured map value type exists.
- Pinning decision recorded (Option A): bitemporal evaluation plus the
  deterministic version hash suffice for v1; explicit `OntologyRef` snapshot
  pinning is deferred (see decision log).

## Motivation

Data and the meaning of data are separate concerns. Meaning must not be
re-derived by guessing at read time, because every consumer (renderer,
validator, importer, search projection) would then guess differently and
cannot be built independently.

Current behavior infers a value kind at write time from a path suffix or from
content (`inferValueKind`): a key named `status` always becomes a status kind
even when it holds prose, and content beginning with `{` or `[` is classified
as JSON even when it is markdown. This subsection removes that inference
entirely. A stored value retains its `ValueKind` (spec 6.9), but key-level
meaning is undeclared, so the kind can drift when content changes and no
consumer has a stable contract to rely on.

This subsection makes meaning declarative, versioned, provenance-bearing, and
enforced by the same machinery as every other missis part.

## 12.11 Declared value kinds and schema parts

## 12.11.1 Mechanism status

Existing today (confirmed by inspection of the model, registry, and CLI):

- the `ValueKind` vocabulary (spec 6.9): text, markdown, scalar, status,
  priority, map, list, ref, code-ref, git-ref, evidence, verification, json,
  artifact, annotation;
- a stored `ValueKind` on every part value;
- `inferValueKind` as the only write-time inference (path suffix
  `status`/`priority`, then JSON-content sniffing, then text) — removed by
  this subsection;
- part-path grammar (spec 7.4): segments `[a-z0-9][a-z0-9._-]*`, joined by
  `/`; enforced by `ValidatePathSegments`; `*` is not a legal segment;
- project and group entities (`missis new --kind project|group`), including
  typed links on them;
- `OpAssignOntology` / `OpRemoveOntology` in the operation registry
  (validated, projection-neutral) with **no runtime loading or enforcement
  yet** (that gap is ticket #27);
- ontology concepts and the illustrative manifest (spec 12.1–12.10);
- the deterministic evaluation cycle (spec 13.1) and the bitemporal winner
  rule (ticket #42).

New in this subsection:

- the `schema` subtree on scope entities as the declaration carrier;
- kind resolution (declared → stored) and its deterministic matching rules;
- write-time and link-time enforcement of declarations;
- the consumer contract for renderers, plugins, importers, and search;
- explicit value kinds on every write, with no inference and no fallback;
- writing parts on project/group entities through the CLI/service set path
  (an implementation prerequisite for #27, not yet implemented).

## 12.11.2 Declaration model

A declaration is a part under the reserved `schema` subtree on a scope entity
(project or group). The declaration's key prefix names the governed ticket
part keys; its value is the declared kind:

```text
schema/<key-prefix> -> <declared kind>
```

A declaration applies to the exact key and to all descendant keys (subtree
inheritance). No wildcard tokens are used: `*` is not a legal path segment
(spec 7.4), and subtree inheritance makes wildcards unnecessary. A more
specific declaration overrides a less specific one at every descendant.

Examples on `project:safedesign`:

```text
schema/status                 -> status
schema/problem                -> markdown
schema/evidence               -> evidence
schema/evidence/run-417       -> code-ref
schema/deps                   -> list[ref]
schema/links/supports         -> ref[ticket|part]
```

Rules:

- `schema/` on scope entities is a reserved namespace for declarations.
  Outside `schema/`, parts on scope entities remain ordinary open-world
  content.
- Tickets never carry declarations. `schema/` on a ticket is ordinary ticket
  content, not a declaration.
- Type-qualified declarations MAY be written as
  `schema/type/<ticket-type>/<key-prefix>` (spec 12.2) and apply only to
  tickets carrying that type. The first segment `type` is reserved: a
  declaration starting with `schema/type/` is always type-qualified, so a
  ticket type name can never be confused with an ordinary key prefix. At
  equal prefix length, a type-qualified declaration beats an unqualified one
  when it applies.

## 12.11.3 Scope and versioning

The effective scope chain for a ticket, in v1, is deterministic:

```text
home-project (via home-project link) -> its groups (canonical-ID order)
```

Overlapping or alternative memberships (spec 14.2) are out of v1 scope and
MUST NOT make resolution nondeterministic; they are a later extension.

Declarations are ordinary parts and therefore:

- bitemporal (effective time and known time follow the ledger rules,
  spec 10.3);
- retractable (removing a declaration is a normal retraction, never a
  delete);
- registry-validated through the same operation registry as every other
  event (ticket #44);
- immutable once accepted — declarations never rewrite event history.

A declaration's provenance identity is the declaring scope entity, the part
path, and the current event alias (for example `@e123`) or event hash. The
effective schema version is a deterministic content hash over the resolved
declaration set. Pinning a schema snapshot through `OntologyRef` (spec 12.9)
is optional future composition, not required by v1.

Enforcement evaluates the effective schema at the proposed event's effective
time. A later declaration change produces re-evaluation, never a rewrite of
historical validity (spec 12.9).

## 12.11.4 Resolution

For a given `(ticket, key, effective time, known time)`:

1. Resolve the applicable declarations from the effective scope chain.
2. Select the matching declaration by, in order:
   a. longest literal key-prefix (most specific);
   b. type-qualified over unqualified at equal prefix length;
   c. nearer scope over farther scope;
   d. the bitemporal winner rule (ticket #42) for the final tie.
   e. canonical declaration order (scope, scope ID, prefix, type, kind,
      event ref) if a, b, c, and d all tie.
3. Resolve the kind, in order:
   a. the selected declaration's declared kind;
   b. the stored `ValueKind` on the value.

There is no third step. Every accepted write carries an explicit
`ValueKind`: declared when a declaration matches, otherwise supplied by the
writer. No key name and no content ever implies a kind; `inferValueKind` is
removed. A write without an explicit kind is rejected even for undeclared
keys, so every implementation computes the same answer from the same two
inputs.

Resolution MUST be deterministic across sessions for the same tuple, so any
session or plugin recomputes the same answer.

## 12.11.5 Declared kinds

Base declarations reuse the existing `ValueKind` vocabulary (spec 6.9).

Composite declarations use a small, kernel-defined grammar written as the
declaration value. In v1, `K` and `V` inside composites are base kinds only;
composites do not nest:

```text
list[K]              list value whose elements are kind K
map[K:V]             map value with key kind K and value kind V
ref[T|U|...]         reference value whose target kind is one of T, U, ...
```

Legal target kinds for `ref[...]`: `ticket`, `part`, `project`, `group`,
`event`, `run`, `code`, `git`, `artifact`.

Relation endpoint legality is declared per relation name:

```text
schema/links/supports -> ref[ticket|part]
schema/links/affects  -> ref[code-ref|ticket|part]
```

Legality constrains which endpoints a typed link may connect (spec 12.1,
12.2). It does not extend the relation vocabulary itself; vocabulary
extension remains ontology work (spec 9.2) and is out of v1 scope. Link
legality composes with link evidence semantics (ticket #66).

A malformed declaration (unknown base kind, malformed composite, illegal
target kind, nested composite) is a validation error on the declaration
write itself. The declaration path parser rejects `schema/type` without a
ticket type and key prefix, and rejects any segment outside the part-path
grammar (spec 7.4), including wildcard characters.

## 12.11.6 Enforcement

Enforcement runs in the deterministic evaluation cycle (spec 13.1, step 6
"validate structure and semantics"):

- A proposed part-value event whose key matches a declaration that conflicts
  with the proposed value is rejected deterministically. The reason names:
  the matched declaration (scope entity + part path + event alias or hash),
  the expected declared kind, and the proposed value.
- On an accepted write under a declaration, the stored `ValueKind` is set to
  the declared kind (its base kind for composites: `list`, `map`, or `ref`),
  so history, projections, and consumers agree. The declared composite
  constrains elements, map key/value kinds, or reference target kinds.
- Undeclared keys remain writable (open world, spec 6.10) with the
  writer-supplied kind; a write without an explicit `ValueKind` is rejected
  (there is no inference fallback).
- Link legality is enforced on assert-link and retract-link: the resolved
  target ref kind must satisfy `schema/links/<relation>` when declared.
- Markdown re-imports are validated under the same rules as direct writes.
- Enforcement is all-or-nothing: a multi-part import or re-import validates
  every proposed part before the batch is appended; any violation rejects
  the whole batch, never a partial success. Appending the batch remains one
  store transaction.
- Every rejection and re-evaluation is explainable by the schema version that
  produced it; enforcement is provenance-bearing.

An implementation MAY combine declaration checks with existing validation,
but the observable ordering and provenance MUST remain deterministic
(spec 13.1).

## 12.11.7 Consumer contract

Renderers, plugins, importers, and search projections receive one resolved
tuple:

```text
(part key, ValueKind, DeclaredSchema{Scope, Pattern, VersionHash, EventRef})
```

`DeclaredSchema` is present only when a declaration matched; otherwise the
consumer sees the stored kind.

Consequences:

- built-in CLI/TUI renderers stop hardcoding per key and render from the
  resolved contract; they never infer from key names or content;
- future renderers and plugins require no kernel changes;
- search projections can extract typed fields from declared keys;
- `list[ref]` is distinguishable from `list[text]`, and `evidence` from
  `markdown`, without content sniffing.

Renderers MUST NOT infer meaning beyond the resolved contract.

## 12.11.8 Boundaries and non-goals

- No external JSON/YAML schema language is introduced. Declarations are
  native missis parts; canonical JSON at rest (spec 10.10) is the storage
  format, not a second authority with its own validator.
- No value, type, permission, or visibility automatically cascades between
  parent and child except the explicitly declared schema-matching rule
  (subtree inheritance); semantic cascades still require an explicit
  declaration (spec 6.10 ontology behavior).
- Declarations never rewrite event history; they are read by projections and
  validators only.
- Delimiter and flattening choices are boundary concerns, not schema
  concerns (ticket #43, `separator-strategy`).
- Relation-vocabulary extension, obligations, and verification hooks remain
  ontology concerns (spec 12.x) tracked by #27; this subsection fixes
  value-kind meaning and relation endpoint legality only.

## 12.11.9 Example

On `project:safedesign`:

```text
schema/status           -> status
schema/problem          -> markdown
schema/deps             -> list[ref]
schema/evidence         -> evidence
schema/evidence/run-417 -> code-ref
schema/links/supports   -> ref[ticket|part]
```

Ticket `#184` (type `bug`, home project `safedesign`) carries:

```text
#184/status               -> "open"
#184/problem              -> "Worker test fails on iteration 417."
#184/deps                 -> ["#12", "#33"]
#184/evidence/race-test   -> "stderr capture"
#184/evidence/run-417     -> "commit abc123"
#184/notes                -> "reran with race detector"
```

Resolution outcomes:

- `#184/status` resolves to `status` from the declaration, even though the
  text "open" is ambiguous on its own;
- `#184/evidence/race-test` resolves to `evidence` by inheritance from
  `schema/evidence`, not by content;
- `#184/evidence/run-417` resolves to `code-ref` via the more specific
  override;
- `#184/notes` is undeclared and keeps the writer-supplied kind (for example
  text or markdown) with no inference (open world);
- writing `#184/deps` with a scalar is rejected with a reason naming
  `project:safedesign/schema/deps -> list[ref]` and the effective schema
  version;
- asserting `#184/evidence/race-test supports -> artifact:xyz` is rejected
  because `artifact` is not a legal target for `supports` under the
  declaration.

## 12.11.10 Decision log

| Decision | Rationale |
| --- | --- |
| Subtree inheritance instead of wildcard segments | `*` is not a legal path segment (spec 7.4); inheritance covers the same need with legal keys and simpler matching. |
| Declarations on scope entities, never in ticket content | Avoids self-governance and namespace pollution; tickets stay open-world content. |
| No inference fallback and no built-in global defaults | A shared guessing heuristic invites divergent implementations and code drift; explicit kinds (declared or writer-supplied) are the single source of truth. |
| v1 scope chain: home-project → groups (canonical-ID order) | Deterministic and implementable; overlapping-membership ordering is a later extension (spec 14.2). |
| Reserved `schema/type/<ticket-type>/<key-prefix>` subtree for type-qualified declarations | A ticket type name would otherwise be ambiguous with an ordinary key prefix. |
| Composite kinds restricted to base kinds in v1 | Keeps the grammar small and unambiguous; nested composites are a later extension. |
| Final tie-break by canonical declaration order after the bitemporal winner | Guarantees identical resolution across sessions when two declarations tie on scope and time. |
| Stored `ValueKind` set to the declared kind on accepted writes | History and consumers agree; projections stay derivable from events. |
| All-or-nothing enforcement for multi-part imports | Prevents partial success; a rejected batch writes nothing. |
| OntologyRef snapshot pinning deferred (bitemporal evaluation + version hash suffice for v1) | Enforcement already evaluates the effective schema at the event's effective time with no retroactive invalidation, and rejection reasons carry the version hash; pinning would add runtime semantics to projection-neutral ops without a near-term consumer. |
| Enforcement at the event's effective time, no retroactive invalidation | Consistent with spec 12.9 and the bitemporal model. |
| Schema version = deterministic content hash of the resolved declaration set | Provenance-bearing without a new counter; `OntologyRef` pinning can layer on later. |
| Relation legality constrains endpoints only | Vocabulary extension is separate ontology work (spec 9.2). |

## 12.11.11 Acceptance criteria

- [ ] A key with a declared kind resolves to that kind regardless of value
      content.
- [ ] A declaration at a prefix applies to descendants (subtree
      inheritance), and a more specific declaration overrides it.
- [ ] Undeclared keys remain writable and resolve through the stored
      `ValueKind` only; there is no implicit kind.
- [ ] A write conflicting with a declaration is rejected with a reason
      naming the declaring scope, the matched pattern, and the effective
      schema version.
- [ ] An accepted write under a declaration stores the declared kind.
- [ ] `schema/links/<relation>` endpoint legality rejects illegal target
      kinds on assert-link.
- [ ] Scope chain and tie-breaks (prefix, type-qualified, scope, bitemporal
      winner) are deterministic across sessions.
- [ ] A write without an explicit `ValueKind` is rejected even for
      undeclared keys (no inference, no fallback; `inferValueKind` removed).
- [ ] Declarations are bitemporal and retractable without rewriting event
      history; a later change does not invalidate earlier writes.
- [ ] Renderers distinguish `list[ref]` from `list[text]` and `evidence` from
      `markdown` from the resolved contract alone.
- [ ] Malformed declarations are rejected at declaration-write time.
- [ ] The declaration path parser accepts
      `schema/type/<ticket-type>/<key-prefix>` and rejects a bare
      `schema/type` or wildcard segments.
- [ ] Two declarations that tie on prefix, type, scope, and time still
      resolve identically in every session (canonical final tie-break).
- [ ] A multi-part import is all-or-nothing: any violation rejects the whole
      batch; no partial success.
- [ ] Elements of a declared `list[ref]` resolve to known references at write
      time; a whole-list `SetValue` under a declared `list[K]` is rejected in
      favor of element-level `--add` writes.
- [ ] Setting parts on project/group scope entities works through the
      CLI/service set path (implementation prerequisite for #27).

## 12.11.12 Implementation status (transient)

The rev 2 core lives in `internal/schema` (declaration grammar, declaration
paths including the reserved `type` subtree, matching and resolution,
enforcement, deterministic version hashing) with 14 unit tests. The
application layer in `internal/application` wires it into the service.

Implemented (2026-08-19):

- scope-entity set path: `missis set project:<id>/<path>` and
  `group:<id>/<path>` write parts; `missis show project:<id>` /
  `group:<id>` renders them; store projections are stream-scoped;
- declaration loader with provenance: `schemaDeclarations` builds
  `schema.Declaration` values (scope, prefix, type-qualified, kind,
  event alias, effective/known times) from real store events, retractions
  excluded, malformed declarations failing loudly;
- enforcement: `SetValue`, `AddValue`, `SupersedeEvent`, `SetLink`, and
  Markdown re-import all validate through the resolver; re-imports are
  all-or-nothing (violations collected, batch rejected, nothing appended);
- no inference fallback: `inferValueKind` is removed; every write carries an
  explicit kind (declared, or writer-supplied via `--kind`/`SetValue.Kind`),
  and a missing kind is rejected;
- renderer integration: `PartView` exposes the resolved kind and
  `DeclaredSchema`; CLI text output and `--json` render from the resolved
  contract via a shared kind dispatch.

Verified by service tests (`internal/application/schema_impl_test.go`) and an
end-to-end CLI test (`testsuite/blackbox/schema_declarations_test.go`);
the full suite passes.

Proven by spike tests:

- a declared kind resolves regardless of value content;
- subtree inheritance and more-specific override;
- undeclared keys resolve to the stored `ValueKind` only, and a missing kind
  stays missing (no implicit text);
- rejection reasons name scope, pattern, expected/proposed kind, and
  effective schema version;
- accepted writes store the declared base kind for composites;
- `schema/links/<relation>` legality rejects illegal target kinds and leaves
  undeclared relations open;
- identical results and version hashes across sessions with shuffled
  declaration order;
- no implicit kind without a declaration (`TestNoImplicitKindWithoutDeclaration`);
- bitemporal selection with no retroactive invalidation;
- malformed kinds and malformed declaration paths rejected;
- type-qualified declarations win at equal prefix and are ignored for other
  ticket types;
- nearest scope wins at equal prefix;
- renderers can distinguish `list[ref]` from `list[text]` and `evidence`
  from `markdown` from the resolved contract.

Remaining for #27 (outside this subsection):

- ontology obligations and verification hooks (spec 12.x);
- plugin renderer hooks (consumers receive the resolved contract today);
- `OntologyRef{ID, Version, Hash}` snapshot pinning on top of the
  deterministic version hash;
- element-level enforcement for `list[K]`/`map[K:V]` values (top-level kind
  and `list[ref]` element resolution are live; `map[K:V]` stays shape-only
  until a structured map value type exists);
- performance: declarations are loaded from the ledger per operation; a
  derived table can serve them once workloads require it (see #51).

## Merge checklist

Do not merge into the main spec until:

- [ ] The decision log is reviewed and adopted (or amended with reasons).
- [x] The declaration grammar and matching rules are confirmed unambiguous
      against the spike implementation (see 12.11.12).
- [ ] No-inference-fallback decision (rev 4) is confirmed against a second
      spike pass and the removal of `inferValueKind` is accepted.
- [ ] Cross-checked against #43 (canonical identity, separator strategy) and
      #66 (evidence semantics).
- [x] The implementation prerequisites (scope-entity set path) are
      enumerated in #27 and implemented.

Then insert as spec section 12.11, update the navigation/change log if the
main spec requires it, and delete this file.
