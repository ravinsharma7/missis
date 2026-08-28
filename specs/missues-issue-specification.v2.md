# Provenance-First Temporal Issue Kernel

## Unified specification for a three-command, agent-friendly and human-friendly issue system

**Status:** Frozen v2 contract (2026-08-27)
**Primary interface:** `missis new`, `missis show`, `missis set`
**Primary storage model:** immutable event ledger  
**Primary content unit:** recursive addressable part  
**Primary connection unit:** typed link  
**Primary semantic layer:** versioned ontology  
**Primary extension mechanism:** provenance-bearing processors and hooks

**Implementation status:** This document contains both the current three-command
contract and aspirational future design. The currently implemented command
surface is the output of `missis --ag-brief` and the Phase 1 requirements. Any
flag or workflow not present there, including `--next`, `--blocked`,
`--obligations`, `--verification`, and `--effects`, is aspirational and MUST
NOT be used as current agent guidance until implemented and tested.
**Revision:** 2026-08-27 — v2 frozen; new protocol work moves to v3-alpha

The v2 contract is frozen. It accepts only correctness, security, data-loss,
and unambiguous documentation corrections needed to preserve its implemented
behavior. New storage protocol capabilities, neutral consumer abstractions,
cross-store references, and intentional behavior changes enter the
authoritative `event-store-v3-alpha.md` contract and its subordinate alpha
contracts. Event-store v3 becomes stable only after the maturity gates there
are met.

---

## Navigation map

- [Core model and three-command CLI](#4-core-conceptual-model)
- [Recursive addressable parts, subparts, and references](#6-addressable-parts-instead-of-one-large-body)
- [Code, Git, links, and lineage](#8-source-code-and-git-references)
- [Temporal event ledger and provenance](#10-immutable-event-ledger-and-temporal-model)
- [Ontology, validation, obligations, and verification](#12-ontology-as-executable-semantics)
- [Hookable processors and plugin lifecycle](#13-deterministic-ontology-and-hook-cycle)
- [Projects, groups, and governance scopes](#14-projects-groups-and-scopes)
- [Large Markdown and incremental updates](#15-large-markdown-and-incremental-part-updates)
- [BM25, vectors, lineage expansion, and reranking](#16-search-architecture)
- [Data model, contracts, security, and persistence](#18-suggested-data-model)
- [End-to-end scenarios and implementation sequence](#26-end-to-end-scenarios)
- [Acceptance criteria](#29-acceptance-criteria)
- [Guarantees and performance](#31-guarantees-and-performance)

## 1. Executive summary

This system presents itself as a very small issue tracker, but internally acts as a provenance-aware workflow and knowledge kernel.

The public command vocabulary is deliberately limited to three domain verbs,
plus a small allowlist of global operational flags:

```text
missis new
missis show
missis set
```

Global operational flags:

```text
missis --version
missis --help
missis --self-update-check
missis --self-update
missis --setup
```

The internal design is based on three lower-level primitives:

```text
EVENT   = an immutable record that something was asserted, changed, observed, or caused
PART    = a recursively nestable, independently addressable unit of ticket information
LINK    = a typed connection between any two addressable references
```

The remaining concepts are interpretations or transformations over those primitives:

```text
LINEAGE      = a provenance or derivation view obtained by traversing links
ONTOLOGY     = executable meaning, constraints, inference, obligations, and verification rules
PROCESSOR    = a plugin that transforms or derives information without hidden mutation
SCOPE        = project or group membership and governance
PROJECTION   = a current, temporal, provenance, search, project, or verification view
```

A ticket is therefore not one mutable row with one large body. It is a temporal container of addressable claims and references:

```text
Ticket
├── identity
├── parts (recursive containment hierarchy)
├── links
├── ontology assignments
├── scope memberships
└── immutable events
```

The authoritative truth is the event ledger. The visible ticket is a projection:

```text
current ticket state = projection(immutable events)
```

This design supports:

- humans entering one large Markdown document;
- agents setting one small part at a time;
- code paths pinned to repositories and commits;
- Git commits, ranges, branches, tags, pull requests, and diffs;
- references between complete tickets, individual parts, events, code, runs, and artifacts;
- bitemporal queries: what happened and what was known at a particular time;
- provenance chains from source to hypothesis to action to code change to evidence;
- ontology-defined validation and verification methodologies;
- hookable pre-processing and post-processing pipelines;
- projects and overlapping groups without duplicating tickets;
- BM25, vector, metadata, symbol, graph, temporal, and hybrid search;
- reranking and lineage expansion;
- embedded, local, or remote search backends;
- rebuildable indexes and derived artifacts;
- an agent protocol small enough to learn completely.

---

## 2. Design goals

### 2.1 Primary goals

1. **Keep the command surface tiny.**
   Domain operations are only `new`, `show`, and `set`. Global operational
   flags are separately allowlisted and do not create new domain verbs.

2. **Be equally usable by humans and agents.**
   Humans may paste or import large Markdown documents. Agents may manipulate exact references and consume stable JSON.

3. **Make provenance the default, not an optional audit feature.**
   Every accepted mutation must identify who or what produced it, when it was recorded, when it became effective, and what it was based on.

4. **Make time first-class.**
   The system must support current, historical, bitemporal, timeline, and temporal-diff views.

5. **Make every useful information unit addressable.**
   A ticket body must not be the only writable unit. Sections, items, evidence, code references, and verification results must be independently addressable. Parts may be nested recursively. A “subpart” is a part in a containment relationship, not a separate entity type.

6. **Make semantic rules replaceable.**
   Different ontologies may define different ticket types, completion rules, evidence requirements, verification methods, and hooks.

7. **Allow virtual composition.**
   Disconnected parts, tickets, projects, commits, artifacts, and runs may be connected through typed links without physically merging them.

8. **Treat search representations as derived projections.**
   BM25 indexes, tokenization, embeddings, summaries, classifications, and reranking scores must be rebuildable from authoritative events and parts.

9. **Avoid implicit agent state.**
   Commands must be understandable without an unstated “current ticket.” Explicit identifiers are carried between operations.

10. **Preserve extension safety.**
    Plugins and hooks propose new events, evidence, links, or effects. They must not perform invisible database mutations.

### 2.2 Non-goals

The kernel does not initially need to reproduce every feature of Jira, GitHub Issues, Linear, or a project management suite.

It does not require:

- dozens of status values;
- a separate command for every mutation;
- one fixed workflow for all projects;
- one mandatory Markdown template;
- one mandatory search engine;
- one mandatory embedding model;
- copying a ticket into every project that references it;
- deleting history to make the current view look clean;
- recording private or hidden reasoning. Observable inputs, claims, actions, evidence, and effects are sufficient.

---

## 3. Normative language

The terms **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** indicate requirement strength.

- **MUST:** required for correctness or compatibility.
- **SHOULD:** strongly recommended; deviations require a documented reason.
- **MAY:** optional behavior.

---

## 4. Core conceptual model

### 4.1 The three user-facing operations

```text
new  : Input → Event+
show : Query × Time × Event* → Projection
set  : Ref × Operation → Event+
```

`new` creates an identity and initial events.  
`show` reads, searches, navigates, explains, and projects.  
`set` proposes changes to existing addressable state.

### 4.2 The three storage primitives

```text
Event = immutable state transition, assertion, observation, or effect record
Part  = logical address inside an entity
Link  = Ref × Relation × Ref
```

A ticket can be summarized as:

```text
Ticket = Tree<Part> + Set<Link> + EventHistory
```

### 4.3 Link versus lineage

The succinct primitive is **link**.

```text
link    = one typed connection
lineage = a path or subgraph derived by traversing relevant links
```

Formally:

```text
Link = Ref × Relation × Ref

Lineage(r, policy, t)
  = reachable references from r
    using links selected by policy
    that are effective at time t
```

A link may connect parts that are otherwise structurally unrelated. This provides the required “virtual connection” without merging identities or moving content.

### 4.4 The issue is a projection, not the source of truth

For a simple valid-time projection:

```text
IssueState(issue, t)
  = fold(events(issue) where effective_at ≤ t)
```

For the full bitemporal projection:

```text
IssueState(issue, valid_time, known_time)
  = fold(
      events(issue)
      where effective_at ≤ valid_time
        and recorded_at ≤ known_time
    )
```

This distinction answers two different questions:

```text
What do we now know happened by 13:00?
What did the system know at 13:00?
```

These are not equivalent.

---

## 5. Stable command-line interface

## 5.1 Commands

```text
missis new
missis show
missis set
```

The implementation MUST NOT require separate top-level subcommands such as:

```text
list get find search next queue inbox close reopen assign comment block prioritize
```

Those behaviors are forms of `show` or `set`.

### 5.1.1 Global operational flags

The binary MAY accept a small allowlist of global flags before a domain verb:

```text
missis --version
missis --help
missis --self-update-check
missis --self-update
missis --setup
```

Global flags are process-level maintenance or inspection operations. They do
not create new domain subcommands, and they MUST NOT become a mechanism for
general ticket operations.

The allowlist MAY grow only for additional process-level maintenance or
inspection flags. It MUST NOT grow for flags that introduce a new command
subpath, mutate ticket state, or start a domain workflow. Those behaviors MUST
still be expressed through `missis new`, `missis show`, or `missis set`.

## 5.2 General command grammar

```text
missis new  [title] [input-options] [metadata-options]
missis show [reference] [view-options] [query-options] [format-options]
missis set  <reference> [value] [mutation-options] [provenance-options]
```

All commands SHOULD support:

```text
--json
--format text|json|markdown
--actor <actor-ref>
--effective-at <timestamp>
```

The system MUST assign `recorded_at` itself. A caller MUST NOT be able to forge transaction time through the normal interface.

### 5.2.1 Reference forms

Reference examples in this specification are suggestive, but every form MUST
resolve to one unambiguous entity or part. The implementation and agent brief
define the accepted spelling for the current release:

```text
#184                         human ticket alias; quote in a shell
184                          shell-safe ticket alias
ticket:01K...                canonical ticket identifier
#184/problem                 ticket part by alias and path
part:01K...                  canonical part identifier
@e114                       event alias
project:safedesign           canonical project identifier
group:security               canonical group identifier
```

Bare ticket numbers and `#N` are equivalent for ticket operations. Canonical
IDs and event aliases are distinct from human aliases; a caller MUST NOT
silently reinterpret one kind as another.

## 5.3 No implicit current ticket

This is invalid as the only protocol:

```text
missis next
missis claim
missis finish
```

because the later commands may depend on hidden terminal state.

The current agent flow carries the ticket ID explicitly:

```bash
missis show --status doing --json
missis set 184/status doing
# perform work
missis set 184/evidence/test-run --from result.json
missis set 184/status done
```

An automatic selector such as `missis show --next --json` is aspirational. It
must not be treated as an implemented replacement for an explicit reference.

Invariant:

```text
Every invocation must be interpretable without prior terminal state.
```

## 5.4 `missis new`

Create from a title:

```bash
missis new "Fix retry race in worker pool"
```

Create with metadata:

```bash
missis new "Fix retry race" \
  --project safedesign \
  --type bug \
  --type concurrency-defect \
  --priority high \
  --tag concurrency
```

Create from Markdown:

```bash
missis new --from bug.md
```

Create from standard input:

```bash
cat bug.md | missis new --stdin
```

Create another entity while preserving the same three-command vocabulary:

```bash
missis new --kind project --id safedesign "SafeDesign"
missis new --kind group --id engineering "Engineering"
```

Example human output:

```text
#184  Fix retry race in worker pool
status: open
project: safedesign
recorded: 2026-08-15T13:03:21+08:00
```

Example JSON output:

```json
{
  "ref": "#184",
  "id": "01J5...",
  "title": "Fix retry race in worker pool",
  "status": "open",
  "project": "safedesign",
  "recorded_at": "2026-08-15T05:03:21Z"
}
```

## 5.5 `missis show`

With no reference, `show` presents the most useful current view:

```bash
missis show
```

Example:

```text
ID    STATUS       PRIORITY   TITLE
184   open         high       Fix retry race
181   doing        normal     Add admission test
176   blocked      normal     Refactor graph cache
```

Show a complete ticket:

```bash
missis show 184
```

Show one part:

```bash
missis show 184/hypothesis
```

Show one exact event:

```bash
missis show @e114
```

Filter or navigate:

```bash
missis show --status open
missis show --tag parser
missis show --blocked
missis show --next
missis show --project safedesign
missis show --group security
missis show --type concurrency-defect
missis show --search "retry race"
```

Temporal and provenance views:

```bash
missis show 184 --history
missis show 184 --at "2026-08-15T13:00:00+08:00"
missis show 184 --effective-at "2026-08-15T13:00:00+08:00" \
               --known-at "2026-08-15T14:00:00+08:00"
missis show 184 --since "2026-08-15T12:00:00+08:00"
missis show 184 --between "13:00..13:15"
missis show 184/status --why
missis show 184 --effects
missis show 184 --references
missis show 184 --lineage
```

Search views:

```bash
missis show --search "retry race" --project safedesign
missis show --search "authentication" --group security
missis show --search "race" --type concurrency-defect --since 7d
missis show --search "retry" --at "2026-07-01"
missis show --search "why was the worker changed" --lineage --explain
```

## 5.6 `missis set`

Set a scalar part:

```bash
missis set 184/status doing
missis set 184/priority high
missis set 184/problem "Worker retries after shutdown."
```

Set a nested or deeply nested part:

```bash
missis set 184/evidence/race-test \
  "go test -race failed at iteration 417"

missis set 184/evidence/race-test/run-417/stderr \
  "Retry was enqueued after context cancellation."
```

Rename or move a part while preserving its canonical identity:

```bash
missis set part:01K2MR7B8Q --name race-detector
missis set part:01K2MR7B8Q --parent 184/verification
```

Retract a complete subtree explicitly:

```bash
missis set 184/evidence/race-test --retract --recursive \
  --reason "Imported under the wrong ticket."
```

Append instead of replace:

```bash
missis set 184/notes --add "Observed on Linux arm64."
```

Set a blocked state with a reason:

```bash
missis set 184/status blocked \
  --reason "Waiting for #171"
```

Add a relationship:

```bash
missis set 219/links --add blocked-by:#184
missis set 220/links --add caused-by:#184
missis set 221/links --add duplicates:#184
```

Add a code reference:

```bash
missis set 184/code --add \
  --repo safedesign \
  --commit 9bd781a82b \
  --path internal/worker/retry.go \
  --lines 118:147
```

Add a symbol reference:

```bash
missis set 184/code --add \
  --repo safedesign \
  --commit 9bd781a82b \
  --path internal/worker/queue.go \
  --symbol Queue.Enqueue
```

Add a Git range:

```bash
missis set 184/git --add \
  --repo https://github.com/acme/safedesign.git \
  --range 813ac22..9bd781a
```

Import or merge Markdown into an existing ticket:

```bash
missis set 184 --from investigation.md
```

Retract rather than delete:

```bash
missis set 184/hypothesis --retract \
  --reason "Contradicted by #184/evidence/test-7"
```

Correct a prior exact event:

```bash
missis set 184/hypothesis \
  "Cancellation happens after enqueue." \
  --supersedes @e100 \
  --because @e118
```

## 5.7 Status model

The kernel SHOULD keep coordination status small:

```text
status ∈ {open, doing, blocked, done}
```

Default transition shape:

```text
open → doing → done
          │
          ▼
       blocked
```

A blocked ticket SHOULD have a reason or dependency.

Status is coordination state, not proof. An ontology may prevent transition to `done` until obligations are satisfied, but SHOULD avoid creating dozens of workflow-specific statuses. Domain detail belongs in parts, obligations, and verification state.

---

## 6. Addressable parts instead of one large body

## 6.1 Part definition

A **part** is an independently addressable information node inside a ticket. A part MAY contain:

```text
value only
children only
value + children
```

A part is not restricted to a leaf node. A **leaf part** is merely a part with no current structural children.

Examples:

```text
#184/problem
#184/context
#184/hypothesis
#184/plan
#184/evidence
#184/evidence/race-test
#184/evidence/race-test/run-417
#184/evidence/race-test/run-417/stderr
#184/code/retry-loop
#184/verification/race-test
```

Every part, at every depth, has the same core capabilities:

- stable canonical identity;
- human-readable current and historical paths;
- independent values and revisions;
- timestamps and provenance;
- ontology types and validation;
- links and backlinks;
- verification and evidence relationships;
- plugin processing;
- search indexing;
- temporal and lineage views.

## 6.2 Recursive model: no special `Subpart` entity

The kernel MUST use one recursive entity type:

```text
Part
```

It MUST NOT require a separate `Subpart` storage type.

“Subpart” describes a relationship or view:

```text
childOf(child, parent)
```

```text
subpartOf(x, y) := childOf⁺(x, y)
```

where `childOf⁺` is the transitive closure of structural containment.

Therefore:

```text
Every subpart is a Part.
Not every Part is currently a subpart.
```

A useful definition is:

```text
Subpart(x, t) := Part(x) ∧ ∃p: containsAt(p, x, t)
```

The word `subpart` MAY appear in user documentation and queries, but it does not create a second class of object.

## 6.3 Conventional sections

The kernel SHOULD recognize common conventions:

```text
problem
context
hypothesis
intent
plan
action
effect
evidence
result
verification
notes
code
git
links
```

These names are conventions, not a mandatory closed schema. Arbitrary parts remain valid unless an ontology forbids them.

Example:

```bash
missis set 184/experiment-1 \
  "Run worker tests 1000 times with the race detector."

missis set 184/result-1 \
  "Failed on iteration 417."
```

## 6.4 Nested and list-like parts

Parts may contain named children to any implementation-supported depth:

```text
evidence
├── race-test
│   ├── command
│   ├── environment
│   ├── run-417
│   │   ├── stdout
│   │   ├── stderr
│   │   └── result
│   └── conclusion
├── production-log
│   ├── source
│   ├── time-range
│   └── observation
└── benchmark
```

Incremental writes remain ordinary `set` operations:

```bash
missis set 184/evidence/race-test/command \
  'go test -race ./internal/worker/...'

missis set 184/evidence/race-test/run-417/result \
  'failed'

missis set 184/evidence/race-test/run-417/stderr \
  'Retry was enqueued after context cancellation.'
```

`missis show 184/evidence` returns the subtree without requiring a separate list or tree command.

## 6.5 Stable identity versus human-readable path

A path is a human-readable, temporal address. It MUST NOT be the canonical identity of a part.

Example:

```text
current path:
  #184/evidence/race-test

canonical identity:
  part:01K2MR7B8Q...
```

The part may later move:

```text
#184/evidence/race-test
    ↓ move
#184/verification/race-test
```

Its canonical identity remains unchanged:

```text
part:01K2MR7B8Q...
```

A move or rename MUST append provenance-bearing events; it MUST NOT silently rewrite historical references.

Example event:

```text
@e201

operation:
  move-part

part:
  part:01K2MR7B8Q

from:
  #184/evidence/race-test

to:
  #184/verification/race-test

recorded-at:
  2026-08-15T14:32:00+08:00
```

Historical paths remain resolvable:

```text
pathAt(part:01K2MR7B8Q, 14:00)
  = #184/evidence/race-test

pathAt(part:01K2MR7B8Q, 15:00)
  = #184/verification/race-test
```

## 6.6 Temporal containment

Parent-child structure is itself temporal and provenance-bearing.

```text
containsAt(parent, child, effective_time, known_time)
```

A part SHOULD have zero or one current **structural parent** inside its ticket. It MAY have any number of non-structural typed links to other parts, tickets, scopes, code references, commits, runs, or artifacts.

A path is a projection over containment and names:

```text
pathAt(part, t)
  = pathAt(parentAt(part, t), t)
    + "/"
    + nameAt(part, t)
```

This lets a historical view reconstruct the hierarchy that existed at a selected time:

```bash
missis show 184 --at "2026-08-15 14:00"
```

Containment changes MUST use events such as:

```text
create-part
attach-child
detach-child
rename-part
move-part
retract-subtree
restore-part
```

These are internal event operations, not new top-level CLI subcommands.

## 6.7 Parent values and child values

A parent MAY contain its own value while also containing children.

Example:

```text
evidence
├── value:
│   "The defect is reproducible through multiple methods."
│
├── race-test
│   └── ...
└── production-log
    └── ...
```

The parent's value is not implicitly equal to the concatenation of descendant values. Search or export layers MAY create a derived subtree aggregate, but that aggregate is rebuildable and non-authoritative.

Likewise, changing a parent value does not implicitly change any child value.

## 6.8 Subpart versus child ticket

A subpart decomposes **information inside one coordination unit**:

```text
#184/evidence/race-test/run-417
```

A linked ticket decomposes **independently actionable work**:

```text
#184 decomposes-into #219
#184 decomposes-into #220
```

Decision rule:

```text
Needs independent coordination
    → ticket

Needs independent addressability only
    → part
```

Independent coordination includes a distinct:

- status;
- owner;
- priority;
- deadline;
- obligation set;
- agent run;
- verification lifecycle.

The kernel SHOULD NOT assign a separate workflow status to every subpart by default. Doing so would create hidden tickets inside a ticket. An ontology may define status-like semantic fields for a part, but the distinction must remain explicit.

## 6.9 Part value kinds

A part MAY contain:

- Markdown or plain text;
- scalar values;
- status or priority values;
- maps or lists;
- references;
- code references;
- Git references;
- evidence objects;
- verification results;
- structured JSON;
- content-addressed artifacts;
- plugin-derived annotations.

The stored value SHOULD retain a declared media type or value kind. Search projections may extract text from supported structured values.

## 6.10 Open-world content with closed-world constraints

Default behavior:

```text
Unknown part names are preserved.
```

Ontology behavior:

```text
Specific ticket types may require, constrain, or interpret selected parts or subtree shapes.
```

This gives:

```text
open-world content
+
closed-world rules where correctness requires them
```

The system MUST NOT discard unrecognized headings, fields, or descendants merely because an ontology does not understand them.

## 6.11 Retraction, removal, and subtree behavior

Retracting a parent value MUST NOT silently destroy its children.

```bash
missis set 184/evidence --retract
```

By default, this retracts only the value stored directly on `#184/evidence`. Descendants remain current:

```text
#184/evidence/race-test
#184/evidence/production-log
```

Removing or detaching an entire subtree must be explicit:

```bash
missis set 184/evidence --retract --recursive
```

Even a recursive removal MUST append retraction or containment events rather than erase history. A recursive request SHOULD be represented as one atomic event batch containing the affected identities and relations.

Conceptually:

```text
batch @b301
├── retract containment: evidence → race-test
├── retract containment: evidence → production-log
├── retract value: evidence
├── retract value: race-test
└── retract value: production-log
```

Historical and provenance views can still reconstruct the subtree.

## 6.12 Hierarchy invariants

The recursive part model MUST preserve these invariants:

1. A part has zero or one current structural parent.
2. A part may have zero or more current structural children.
3. Structural containment is acyclic.
4. A part retains its canonical identity when moved or renamed.
5. Parent-child relations are temporal and provenance-bearing.
6. Every part is independently readable, writable, linkable, searchable, versioned, typed, and verifiable.
7. No value, type, permission, status, verification result, retention rule, or search visibility automatically cascades between parent and child unless an ontology or policy explicitly declares that behavior.
8. Work decomposition uses ticket-to-ticket links, not hidden subpart workflows.
9. Human-readable paths are unique within a ticket under one selected time projection.
10. Moving a part cannot make it its own ancestor.

Acyclicity is:

```text
∀p: ¬descendantOf(p, p)
```

The system MUST reject structures such as:

```text
A contains B
B contains C
C contains A
```

---

## 7. Universal reference model

## 7.1 Human-readable references

Logical ticket:

```text
#184
```

Logical part:

```text
#184/problem
#184/evidence/race-test
```

Project-scoped alias:

```text
safedesign#184
safedesign#184/code/retry-loop
```

Exact immutable event:

```text
@e114
```

Project and group:

```text
project:safedesign
group:engineering
```

Agent or process run:

```text
run:codex/238
run:test/773
```

Artifact:

```text
artifact:sha256:...
```

## 7.2 Logical reference versus event reference

```text
#184/hypothesis
```

means:

> The logical part currently addressed as `hypothesis` on ticket 184, viewed under the selected time projection.

```text
@e114
```

means:

> This exact immutable historical event.

The distinction is mandatory because logical parts evolve while event identities never change.

## 7.3 Canonical internal identity

Human references and paths are aliases. Internally, entities SHOULD use globally unique immutable identifiers such as UUIDs or ULIDs.

Example mappings:

```text
safedesign#184
    → ticket:01J5...

#184/evidence/race-test
    → part:01K2MR7B8Q...
```

Renaming a project, changing a human ticket number, renaming a part, or moving a part MUST NOT change canonical identity.

A canonical reference SHOULD be able to resolve independently of the current path:

```text
part:01K2MR7B8Q...
```

## 7.4 Part path syntax and resolution

Recommended part segment syntax:

```text
[a-z0-9][a-z0-9._-]*
```

Segments are separated by `/`. Display names with spaces MAY be stored separately. Explicit Markdown identifiers SHOULD override generated slugs.

A logical path resolves under a selected temporal projection:

```text
resolvePath(ticket, path, effective_time, known_time) → PartID
```

Therefore the same text path may denote different historical placements only when the query also specifies time. Within one ticket and one selected projection, a current path MUST resolve to at most one current part.

Deep references are ordinary logical references:

```text
#184/evidence/race-test/run-417/stderr
```

A stale path MUST NOT silently retarget to a different part that later reuses the same text path. A resolver SHOULD do one of the following:

- resolve the path under an explicitly selected historical time;
- resolve its canonical identity and report the current moved or renamed path;
- reject an ambiguous lookup and show candidate canonical IDs.

Stored links and provenance SHOULD target canonical `PartID` values. The human-readable path used when a link or event was created MAY also be retained for explanation.

---

## 8. Source-code and Git references

## 8.1 Stable source coordinate

A source reference SHOULD be pinned to an immutable repository state:

```text
(repository, commit, path, selector?)
```

Formally:

```text
CodeRef = Repo × Commit × Path × Selector?
```

A path without a repository and commit is weak because it cannot answer which version was inspected.

## 8.2 Human-readable source syntax

File:

```text
safedesign@9bd781a:internal/worker/retry.go
```

Line range:

```text
safedesign@9bd781a:internal/worker/retry.go:118-147
```

Symbol:

```text
safedesign@9bd781a:internal/worker/retry.go#RetryLoop
```

The parser SHOULD support a canonical structured form even if shorthand display varies.

## 8.3 Selectors

A code reference MAY select:

- an entire repository;
- a directory;
- a package or module;
- a file;
- a line or byte range;
- a symbol;
- an AST node;
- a CFG node;
- a DFG node;
- a call-graph node;
- another SafeDesign graph identity.

Line numbers are convenient but drift. Symbol or syntax-node identities SHOULD be used when available, while retaining the original file and range as provenance.

## 8.4 Git references

A Git reference MAY identify:

- repository;
- commit;
- commit range;
- branch;
- tag;
- pull request;
- diff;
- tree;
- blob.

Example:

```text
repo: github.com/acme/safedesign
base: 813ac22
head: 9bd781a
```

A branch is mutable. When a branch is recorded, the resolver SHOULD also record the commit it resolved to at that time.

## 8.5 Future SafeDesign integration

The code-reference structure SHOULD be extensible:

```text
SourceRef
├── repo
├── commit
├── file
├── range
├── symbol
├── AST node
├── CFG node
├── DFG node
└── graph node
```

The ticket interface does not need to change when richer selectors become available.

---

## 9. Typed links and virtual connections

## 9.1 Link definition

```text
Link = Ref × Relation × Ref
```

Examples:

```text
#184/evidence/race-test
    supports → #184/hypothesis

#185/evidence/test-1
    contradicts → #184/hypothesis

#219/problem
    blocked-by → #184

#184/code/retry-loop
    resolves-to → graph-node:83719
```

## 9.2 Relation vocabulary

The kernel SHOULD provide a minimal built-in vocabulary and permit ontologies to define additional relations.

Useful relations include:

```text
blocks / blocked-by
caused-by / causes
duplicates / duplicated-by
supersedes / superseded-by
implements / implemented-by
discovered-from / discovered
derived-from / derives
supports / supported-by
contradicts / contradicted-by
verified-by / verifies
affects / affected-by
motivated-by / motivates
resolves-to / resolved-from
same-origin
related
contains / contained-by
governs / governed-by
has-home / home-of
member-of / has-member
```

## 9.3 Inverse maintenance

Where a relation has a declared inverse, the system SHOULD derive the inverse automatically.

Example invariant:

```text
blockedBy(A, B) ⇔ blocks(B, A)
```

The inverse may be a derived projection rather than a second authoritative assertion.

## 9.4 Links are temporal

Adding or retracting a link creates events.

At 13:20:

```text
#219 blocked-by #184
```

At 14:15 the relation is retracted.

Therefore:

```bash
missis show 219 --at 13:45
```

may show the blocker, while:

```bash
missis show 219 --at 15:00
```

may not.

## 9.5 Asserted and derived links

A link projection MUST indicate whether it was:

- explicitly asserted by a human or agent;
- inferred by an ontology;
- derived by a processor;
- imported from an external artifact.

Derived links MUST retain the source events and processor invocation that produced them.

## 9.6 Virtual cross-project connection

Example:

```text
safedesign#184/problem
    same-origin → aici#73/problem
```

The tickets remain separate, maintain separate home projects, and preserve separate histories. The link permits shared search, lineage traversal, and reasoning across project boundaries.

This v2 example assumes both projects resolve inside the **same authoritative
store**. A project label is not a store identity. Two repository-local stores
may both allocate `#184`, and may even contain independently generated entity
IDs; `safedesign#184` therefore MUST NOT be serialized as a canonical
cross-store reference.

Canonical cross-store identity, durable external-reference values, peer
construction, resolution, and their implementation status are outside frozen
v2. They are governed only by the authoritative versioned
`event-store-v3-alpha.md` and `cross-store-references-v3-alpha.md` contracts.
New cross-store behavior MUST NOT be added normatively to this document.

## 9.7 Lineage view

A lineage view traverses selected provenance-relevant relations:

```text
source code
    ↓ derived-from
hypothesis
    ↓ motivated
plan
    ↓ produced-by
action
    ↓ caused
commit
    ↓ verified-by
test evidence
```

Lineage queries SHOULD support:

- direction;
- maximum depth;
- relation allow-list or deny-list;
- time projection;
- scope filtering;
- asserted-only or include-derived;
- path explanation.

Example:

```bash
missis show '#184/hypothesis' --lineage \
  --direction both \
  --depth 4 \
  --relations derived-from,supports,contradicts,verified-by
```

---

## 9.8 Atomic link workflows and link-assertion preconditions

### 9.8.1 MoveLink

`MoveLink` moves an active membership assertion so its origin changes from
`From` to `To` while the other endpoint stays `Target`. It appends
`retract-link` + `assert-link` in one atomic batch (multi-stream batches are
supported by the store; ticket #77).

| Relation | Retracted assertion | Asserted assertion | Events live on |
| --- | --- | --- | --- |
| `has-home` | `(Target, has-home, From)` | `(Target, has-home, To)` | Target stream |
| `contains` | `(From, contains, Target)` | `(To, contains, Target)` | From and To streams |
| `governs` | `(From, governs, Target)` | `(To, governs, Target)` | From and To streams |

Validation (nothing written on failure): the relation must be a membership
relation (`has-home`, `contains`, `governs`); `From`, `To`, and `Target` must
resolve to existing refs; `From` and `To` must differ; per-relation endpoint
rules apply; an active assertion must exist; a caller-supplied `IfCurrent`
alias must match the current assertion event, otherwise a conflict with retry
guidance. The result reports the transition and never emits the zero-home
warning, because the intermediate state does not exist.

### 9.8.2 Link-assertion precondition

The store precondition vocabulary has two forms:

1. part/entity expected-current-event (`--if-current`);
2. link-assertion expected-current-event: the batch applies only if the
   expected event is still an active assertion of `(From, Relation, To)`;
   otherwise a conflict. Multiple assertions of the same triple coexist and
   each may be guarded independently (evidence semantics, #66).

`MoveLink` attaches the link-assertion precondition automatically, reading
the current assertion at effective time; callers may override via
`IfCurrent`. Unguarded moves are rejected with guidance rather than silently
proceeding. Preconditions are per-request and never stored.

### 9.8.3 Terminology: assertion, declaration, precondition

These three terms are distinct and must not be interchanged, even though
they overlap in mechanism:

- **Assertion** (link assertion): a recorded claim that a relation holds
  between two refs. The record is an `assert-link` event with provenance
  (actor, recorded/effective time, source); `retract-link` withdraws one
  assertion. Assertions are evidence: the visible relation is derived from
  them (visible while at least one is active). Used for membership
  (`has-home`, `contains`, `governs`) and any typed relation.
- **Declaration** (schema declaration): a part under the reserved `schema/`
  subtree on a scope entity (project/group) that maps a key prefix to a
  declared value kind (for example `schema/status -> status`). Declarations
  are meaning rules for part keys, not claims about relations (section 12.11,
  tracked by #27).
- **Precondition**: a write-time guard, not a fact. A precondition names an
  expected current event; the mutation applies only while that expectation
  holds, otherwise a conflict. Two forms: part/entity
  expected-current-event (20.1) and link-assertion expected-current-event
  (9.8.2).

Overlap: all three are ledger records, bitemporal, retractable, and
provenance-bearing. Differences: assertions claim a relation between refs;
declarations assign meaning to part keys; preconditions constrain when a
write may apply. Assertions and declarations may be retracted; preconditions
are per-request and never stored.

The full guarantees and performance inventory lives in section 31.

---

## 10. Immutable event ledger and temporal model

## 10.1 `set` never destructively overwrites truth

Command:

```bash
missis set 184/status doing
```

creates an event similar to:

```text
event: @e101
issue: #184
path: /status
operation: set
before: open
after: doing
recorded_at: 2026-08-15T13:03:21+08:00
effective_at: 2026-08-15T13:03:21+08:00
actor: agent/codex/run-7
```

The visible status is computed from events.

## 10.2 Two time axes

Every accepted event MUST contain:

```text
recorded_at
```

Every accepted event MUST also contain or default:

```text
effective_at
```

Definitions:

- `recorded_at`: when the ledger accepted the event; assigned by the system.
- `effective_at`: when the assertion, state, or effect became true in the represented domain.

Example:

At 14:00, an agent discovers that a crash occurred at 12:37:

```text
recorded_at  = 14:00
effective_at = 12:37
```

Formally:

```text
knownAt(x, t) ≠ happenedAt(x, t)
```

The common `--at` flag MAY set both query axes. Advanced queries SHOULD expose `--effective-at` and `--known-at` separately.

## 10.3 Time storage and display

- Timestamps MUST be stored in UTC or another unambiguous absolute representation.
- Human output SHOULD render in the selected user or project timezone.
- Text and JSON output MUST use ISO 8601 / RFC 3339-compatible timestamps.
- Event ordering within an entity stream MUST be deterministic even if timestamps are equal. A per-stream sequence number SHOULD be used.

## 10.4 Event identity

Every event MUST have a globally unique immutable ID.

Sortable IDs such as ULIDs are convenient but not mandatory. Human aliases such as `@e114` may map to a canonical event ID.

## 10.5 Retraction instead of deletion

A retraction means the value is no longer current, not that it never existed.

```text
retracted(x) ⇒ ¬current(x)
retracted(x) ⇏ never_existed(x)
```

The system SHOULD use retraction events for:

- removing a current part;
- removing a link;
- withdrawing a claim;
- removing a scope membership;
- deactivating an ontology assignment.

Administrative erasure for legal or privacy requirements is a separate exceptional mechanism and MUST leave an auditable tombstone where legally permitted.

## 10.6 Corrections and supersession

A correction SHOULD identify what it supersedes and why:

```text
@e120
supersedes: @e100
value: Cancellation happens after enqueue.
because: @e118
```

This permits evaluation of how claims evolved and prevents silent history rewriting.

## 10.7 Temporal diff

Example:

```bash
missis show 184 --between "13:00..13:15"
```

Possible output:

```text
13:02 + evidence/race-test
13:11 ~ code/retry-loop
13:11 + git/fix
13:14 + verification/race-test
13:15 ~ status
```

A semantic temporal diff SHOULD show changed parts, changed links, added evidence, Git state transitions, and status transitions rather than only raw event records.

## 10.8 Event batches

One Markdown import or one processor invocation may produce multiple events. These SHOULD share a batch ID.

Default import behavior SHOULD be atomic:

```text
all proposed events validate and append
or
none append
```

A partial-import mode MAY exist but must be explicit and produce a rejection report for omitted parts.

## 10.9 Bitemporal winner rule (decided 2026-08-17)

For scalar values, the visible value at valid time `V` and known time `K` is:

```text
Candidates(x, V, K) =
    { e : target(e) = x
          and recorded_at(e) <= K
          and effective_at(e) <= V
          and not superseded_as_of(e, K) }

Winner(x, V, K) = argmax over Candidates of
    (effective_at, recorded_at, stream_sequence, event_id)
```

Semantics:

- Each assertion applies from its `effective_at` until a candidate with a
  strictly greater tuple wins (historical-correction model). A backdated
  assertion changes only the interval it names.
- Bounds are inclusive: `recorded_at <= K` and `effective_at <= V`.
- A retraction at effective time `T` means no value from `T` until a later
  assertion wins; it does not mean the value never existed. The retraction is
  invisible at known times before it was recorded. "No value" is an interval
  hole, not a tombstone.
- `Supersedes(new, old)` targets the same canonical target and operation
  family. The superseded event is void in any projection where the superseder
  is known, even before the superseder's own effective time.
- Projection fields: `CurrentFrom` is the event that established the value at
  `(V, K)`; `RetractedBy` is the retraction opening the current hole, if any.
- Projections fold events ordered by `(effective_at, recorded_at,
  stream_sequence, event_id)`. Stream sequence remains the order of
  acceptance; it never replaces valid time for projection.

## 10.10 Canonical event encoding v1 (decided 2026-08-17)

Event hashing and cleanroom portability use one canonical byte form:

```text
CanonicalEventBytes(event, v1) -> byte string
```

Contract:

- Format: canonical JSON (RFC 8259), UTF-8, no HTML escaping, deterministic
  key order, no duplicate keys, no NaN/Infinity.
- Top-level fields included, in order: `ID`, `Stream`, `Sequence`,
  `BatchID`, `Operation`, `Target`, `Value`, `RecordedAt`, `EffectiveAt`,
  `Actor`, `Sources`, `Inputs`, `Causes`, `Effects`, `Supersedes`, `Reason`,
  `Ontologies`, `Invocation`.
- Excluded, never hashed: `AliasSeq`, `PreviousHash`, `Hash`.
- Nested object key order follows the reference schema (the Go model types);
  published test vectors pin the exact bytes.
- Timestamps: UTC, exactly 9 fractional digits, `Z` suffix, trailing zeros
  retained, e.g. `2026-08-17T02:40:29.123456789Z`.
- Integers are JSON integers. `Value.Data` must be JSON-safe; object keys are
  sorted.
- Closed schema per version: unknown fields make the event invalid for v1.
- Hash framing is domain-separated:

```text
HashInput  = "MISSIS-EVENT-HASH" || 0x00 || "v1" || 0x00
             || previous_hash || 0x00 || canonical_bytes
event_hash = SHA-256(HashInput)
```

- Existing chains are not reinterpreted. v1 applies to new chain segments; a
  format-versioned migration is a separate storage task.
- Reference implementation: `model.CanonicalEventBytesV1` and
  `model.ComputeEventHashV1`; test vectors live in
  `internal/model/canonical_test.go`.

The human-readable storage-compatibility summary lives in
`docs/storage-compatibility.md`; any change to the on-disk format (new
migration, canonical encoding, derived tables) must update that document
alongside this spec.

---

## 11. Provenance-first model

## 11.1 Required questions

For every current value or derived claim, the system should be able to answer:

```text
Who or what created this?
When was it recorded?
When did it become effective?
What source was inspected?
What inputs were consumed?
What caused the operation?
Which processor or ontology rule derived it?
What changed because of it?
Which evidence verifies or contradicts it?
```

## 11.2 Event provenance fields

An event SHOULD support:

```text
actor
sources[]
inputs[]
causes[]
effects[]
supersedes[]
processor_invocation
ontology_versions[]
batch_id
```

Meaning:

- **actor:** human, agent, process, hook, import, or system component responsible for the proposal.
- **source:** artifact, source span, code reference, Git reference, command output, external record.
- **input:** data consumed to produce the event.
- **cause:** event or obligation that triggered the action.
- **effect:** observed external or internal result.
- **supersedes:** exact earlier events corrected or replaced in the current projection.

## 11.3 Intention, action, effect, observation, and verification are distinct

```text
intent       = what was meant to be accomplished
action       = what operation was performed
effect       = what state actually changed
observation  = what was measured or witnessed
verification = ontology-defined judgment over evidence
```

These MUST NOT be collapsed into one “agent note.”

Example:

```text
intent:
  Fix retry race.

action:
  Moved cancellation check before enqueue.

effects:
  Modified internal/worker/retry.go.
  Created commit afe2821.

observation:
  10,000 race-test iterations passed.

verification:
  concurrency-fix verified under concurrency-bug@2.
```

An action may fail to create the intended effect:

```text
action: kill worker process
effect: process survived
```

## 11.4 Agent and process runs

Runs SHOULD have stable references:

```text
human/ravin
agent/codex/run-238
agent/claude/run-92
process/test/run-773
system/git-hook/run-91
```

A run record MAY contain:

- actor type;
- harness and model identifiers;
- tool invocations;
- commands;
- workspace;
- initial repository commits;
- granted capabilities;
- input references;
- output events;
- observed effects;
- start and end timestamps;
- exit status;
- environment fingerprint.

The system does not need hidden internal reasoning. It should preserve observable claims, decisions, inputs, actions, evidence, and results.

## 11.5 Code-change lineage example

```text
safedesign@91fa1c2:internal/worker/retry.go#retryLoop
        ↓ derived-from
#184/hypothesis
        ↓ motivates
#184/plan
        ↓ executed-by
agent/codex/run-238
        ↓ produces
safedesign@afe2821
        ↓ tested-by
process/test/run-773
        ↓ produces
#184/evidence/race-test
        ↓ verifies
#184/verification
```

Every edge in this chain is referencable and temporally queryable.

## 11.6 Effects

Effects MAY include:

```text
code
Git
file system
process
network
external API
database
test
build
deployment
dependency
scope membership
ontology-derived obligation
```

If an operation is expected to change external state, its observed effects SHOULD be recorded.

Core provenance shape:

```text
input → action → effect
```

not merely:

```text
actor → note
```

## 11.7 Integrity

The ledger MAY use event hashes or per-stream hash chaining:

```text
hash(event_n) includes hash(event_n-1)
```

This is recommended when tamper evidence matters. Integrity metadata must not prevent authorized redaction workflows required by law or policy.

---

## 12. Ontology as executable semantics

## 12.1 Purpose

An ontology does more than label a ticket. It defines:

- entity and part types;
- legal relationships;
- required parts;
- validation constraints;
- inference rules;
- obligations;
- completion contracts;
- verification methods;
- search relation semantics;
- processor hooks;
- capability policies;
- inheritance and composition rules.

## 12.2 Typed tickets and parts

Example:

```text
#184
  type: bug
  type: concurrency-defect
  type: go-code-change
```

Possible semantic definitions:

```text
Bug
├── requires → problem
├── may-have → reproduction
├── may-affect → CodeRef
├── fixed-by → CodeChange
├── verified-by → Evidence
└── done requires → Verification
```

```text
Experiment
├── requires → hypothesis
├── requires → method
├── produces → observation
├── produces → result
└── done requires → conclusion
```

Therefore:

```text
done(Bug) ≠ done(Experiment)
```

A completed experiment does not require its hypothesis to be true. It requires the method to be executed and the result recorded.

### Recursive part constraints

Ontologies MAY validate hierarchy shape and descendant relationships.

Examples:

```text
RaceTest(x)
  → requiresChild(x, Command)
  ∧ requiresChild(x, Environment)
  ∧ requiresChild(x, Result)
```

```text
EvidenceItem(x)
  → ∃s: childOf(s, x) ∧ Source(s)
```

```text
Fix(x)
  → ∃v:
      descendantOf(v, VerificationSection)
      ∧ verifies(v, x)
```

Useful structural constraints include:

```text
/evidence/* must have a source
/verification/* must reference the claim it verifies
/code/* must contain repository and immutable commit
/experiment/* must contain method and result
```

Hierarchy does not imply semantic inheritance unless explicitly declared. For example:

```text
Confidential(x)
∧ descendantOf(y, x)
  → Confidential(y)
```

is an explicit policy rule, not a built-in assumption.

The same rule applies to permissions, ownership, status, verification state, retention, and search visibility. Ontology-defined cascades MUST be deterministic, versioned, explainable, and provenance-bearing.

## 12.3 Validation versus verification

Validation asks:

> Is the proposed entity, part, link, or event structurally and semantically legal?

```text
Valid(ontology, current_state, proposed_event) → ValidationResult
```

Examples:

- a bug requires `/problem`;
- a `CodeRef` requires repository, commit, and path;
- `blocked-by` must target another ticket;
- a ticket cannot block itself;
- a high-risk security finding requires a severity value;
- a verification result must name a method and evidence.

Verification asks:

> Does the available evidence sufficiently support a particular claim under a declared method?

```text
Verify(ontology, claim, evidence[]) → VerificationResult
```

Critical relation:

```text
valid(ticket) ⇏ verified(ticket)
```

A ticket may be perfectly valid while its fix remains unverified.

## 12.4 Obligations

Ontologies SHOULD produce explicit obligations instead of only rejecting malformed forms.

Example:

```text
#184 obligations

✓ problem
✓ affected-code
✓ remediation
○ independent-review
○ remediation-verification
```

A useful rule:

```text
SecurityFinding(x)
∧ Severity(x) ≥ High
    → Requires(x, IndependentReview)
```

`missis show --next` may choose the highest-priority unsatisfied obligation.

## 12.5 Completion contracts

Example:

```yaml
ontology: bug@3
required:
  - problem

done_requires:
  - fix
  - verification

verification_requires_one_of:
  - automated-test
  - proof
  - reproducible-observation
  - explicit-review
```

Attempt:

```bash
missis set 184/status done
```

may be rejected:

```text
cannot transition #184 to done

missing obligation:
  verification

acceptable evidence:
  automated-test
  proof
  reproducible-observation
  explicit-review
```

The ontology defines the contract; the kernel does not hardcode one universal definition of done.

## 12.6 Verification methodologies

### Concurrency defect

Possible methods:

```text
race-test
stress-reproduction
model-check
schedule-exploration
runtime-trace-review
```

Example rules:

```text
completion requires at least one concurrency-sensitive method
high-risk completion requires two independent methods
```

### Performance regression

Possible requirements:

```text
benchmark-before
benchmark-after
environment-equivalence
statistical-comparison
regression-threshold
```

### Data migration

Possible requirements:

```text
pre-count
post-count
checksum or invariant
rollback-plan
rehearsal evidence
```

### Security finding

Possible requirements:

```text
affected code
severity classification
threat or exploit precondition
remediation
independent review for high severity
remediation verification
```

### Experiment

Possible requirements:

```text
hypothesis
method
inputs
observations
result
conclusion
```

A negative or inconclusive result may still satisfy the experiment completion contract.

## 12.7 Verification result states

A verification result SHOULD be more expressive than a Boolean:

```text
unverified
verified
failed
inconclusive
stale
```

A result may become stale when its referenced code commit, environment, input data, or ontology method changes. Staleness should be derived and provenance-bearing, not silently applied.

## 12.8 Ontology composition

A ticket may combine several ontologies:

```text
Bug
+ ConcurrencyDefect
+ GoCodeChange
```

Formally:

```text
O_effective = O_bug ∪ O_concurrency ∪ O_go
```

Composition SHOULD be monotonic by default:

```text
A child ontology may add requirements.
A child ontology must not silently remove parent requirements.
```

An exemption or override MUST be explicit, scoped, versioned, and provenance-bearing.

## 12.9 Ontology versioning

Never store only:

```text
ontology: bug
```

Store:

```text
ontology: bug@3
hash: sha256:...
```

This permits:

```text
valid_under(#184, bug@2, 2026-07-01)
valid_under(#184, bug@3, 2026-08-15)
```

A change in ontology must not rewrite historical validity. Re-evaluation under a new ontology produces a new evaluation event or projection.

Example:

```text
historically valid under bug@2
current ontology bug@3:
  missing independent regression evidence
```

## 12.10 Example ontology manifest

```yaml
id: concurrency-bug
version: 2
extends:
  - bug@3
  - go-code-change@7

recognizes:
  parts:
    - problem
    - hypothesis
    - evidence/*
    - code/*
    - verification/*

requires:
  - part: problem
  - any_of:
      - part: evidence/reproduction
      - part: evidence/log
  - at_least: 1
    relation: affects
    target_type: CodeRef

transitions:
  done:
    requires:
      - obligation: fix-recorded
      - verification: concurrency-fix

verification_methods:
  concurrency-fix:
    any_of:
      - method: race-test
      - method: stress-reproduction
      - method: model-check

hooks:
  - phase: post.part
    when: part.type == "CodeRef"
    processor: safedesign/source-impact-analysis@3

search:
  relation_weights:
    supports: 1.0
    derived-from: 1.0
    related: 0.25
```

The manifest format is illustrative. The semantic model matters more than YAML specifically.

---

## 13. Deterministic ontology and hook cycle

## 13.1 Evaluation cycle

The system SHOULD expose a deterministic lifecycle:

```text
1. receive proposed input or event
2. resolve scope and effective ontology
3. run pre-ingest processors
4. normalize into proposed parts, links, and events
5. run pre-part processors
6. validate structure and semantics
7. append accepted event batch
8. derive facts and inverse links
9. derive obligations
10. run applicable post hooks
11. collect evidence and observed effects
12. evaluate verification methods
13. update materialized projections
14. update or enqueue search projections
```

An implementation MAY combine phases, but the observable ordering and provenance must remain deterministic.

## 13.2 Hook points

Useful hook phases include:

```text
pre.ingest
pre.part
pre.child
pre.validate
post.event
post.part
post.child
post.descendant
post.subtree
post.link
post.obligation
post.verify
post.scope
post.projection
```

Examples:

```text
on Part created
    → process one exact part

on Child added
    → validate the immediate parent-child relation

on Descendant changed
    → re-evaluate affected subtree obligations

on Subtree completed
    → produce a derived summary or verification proposal

on CodeRef added
    → resolve symbol

on CodeChange observed
    → calculate graph impact

on VerificationRequested
    → execute permitted tests

on Commit attached
    → attach diff and changed paths

on Done proposed
    → evaluate unresolved obligations
```

## 13.3 Processor contract

A processor MUST NOT silently mutate authoritative storage.

Preferred contract:

```text
Processor(Input) → ProposedOutput
```

Where:

```text
ProposedOutput =
    proposed parts
  + proposed links
  + annotations
  + evidence
  + effects
  + diagnostics
  + obligations
```

All accepted outputs become events with provenance.

A processor MAY receive one exact part or an explicitly bounded subtree:

```text
exact part:
  #184/evidence/race-test/run-417/stderr

subtree selector:
  #184/evidence/race-test/**
```

Subtree processing MUST record the selected root, input revisions, traversal policy, depth and item limits, and all emitted outputs. A processor MUST NOT secretly rewrite descendants.

## 13.4 Pure and effectful processors

### Pure processor

Consumes inputs and produces deterministic derived output.

Examples:

- Markdown parser;
- ontology classifier;
- token extractor;
- source-symbol resolver over a fixed commit;
- embedding generator over fixed model and input;
- inverse-link derivation.

### Effectful processor

May inspect or modify external state.

Examples:

- run tests;
- modify a repository;
- call an external API;
- create a commit;
- deploy a build.

Effectful processors MUST declare capabilities and record observed effects.

## 13.5 Capability-based execution

Example:

```yaml
processor: verify-race-fix@2
capabilities:
  allow:
    - repo.read
    - process.test
  deny:
    - repo.write
    - network
```

A separate implementation processor might receive:

```yaml
capabilities:
  allow:
    - repo.read
    - repo.write
    - process.test
```

Default policy SHOULD be least privilege. Network, credentials, shell, repository writes, and external side effects SHOULD be denied unless explicitly granted.

## 13.6 Processor provenance

Each invocation SHOULD record:

```text
invocation ID
processor ID and version
processor code hash
configuration hash
input references
input event IDs
ontology versions
capabilities granted
start and end timestamps
output references
observed effects
exit status
diagnostics
```

Example explanation:

```text
Why does #184 say queue.go is affected?

Because source-impact-analysis@3
processed @e199
against safedesign@abc123
and produced @e201.
```

## 13.7 Pipeline composition

Processors SHOULD form an explicit dataflow or DAG:

```text
Markdown
   ↓ markdown-parts@2
Parts
   ↓ ontology-classifier@4
Typed parts
   ↓ code-ref-resolver@7
Resolved references
   ↓ dependency-expander@3
Links
   ↓ embedding@bge-m3
Search vectors
```

Ordering MUST be inspectable. A system MUST NOT hide critical behavior inside an undocumented callback order.

## 13.8 Idempotency and cycle control

Processor idempotency key SHOULD include:

```text
processor ID
processor version
configuration hash
input event IDs
```

The hook engine MUST prevent infinite recursion through one or more of:

- causal-cycle detection;
- maximum derivation depth;
- idempotency keys;
- duplicate output detection;
- explicit “derived from” ancestry checks.

A prevented cycle SHOULD produce a diagnostic event or visible hook result.

## 13.9 Failure policy

- A failing pre-validation processor may reject the proposal.
- A failing optional post-processor should not erase the already accepted source event.
- Hook failure SHOULD create a diagnostic and, where policy requires it, an obligation.
- Retrying a processor MUST preserve invocation provenance and avoid duplicate derived facts.

---

## 14. Projects, groups, and scopes

## 14.1 Scope abstraction

```text
Scope
├── Project
└── Group
```

Projects and groups are entities with events, parts, links, ontology assignments, memberships, and temporal views.

## 14.2 Overlapping membership

A project may belong to several groups:

```text
project:safedesign
  member-of → group:engineering
  member-of → group:security
  member-of → group:open-source
```

A group may contain projects or other groups. The model should be a graph, not a mandatory single tree.

## 14.3 Grouping versus governance

The relations MUST distinguish organization from semantic authority.

```text
contains
```

means:

> Include this entity in navigation, reporting, or search scope.

```text
governs
```

means:

> Apply ontology, validation, verification, hook, or policy inheritance.

Example:

```text
group:engineering governs project:safedesign
group:security contains project:safedesign
```

Adding a project to a dashboard or reporting group must not accidentally change whether its tickets are valid.

## 14.4 Home project and additional memberships

A ticket SHOULD have one home project for human numbering and default governance:

```text
has-home: project:safedesign
```

It MAY appear in additional projects or groups:

```text
also-in:
  project:aici
  group:security-review
```

The system should preserve:

```text
one canonical ticket identity
one provenance history
multiple scoped views
```

It should avoid creating independent copies such as:

```text
SAF-184
AICI-92
SEC-17
```

when all three represent the same underlying ticket.

## 14.5 Effective ontology resolution

A practical rule:

```text
effectiveOntology(ticket)
  = ontology(governing groups)
  ∪ ontology(home project)
  ∪ ontology(ticket types)
  ∪ explicit ticket ontology assignments
```

Example:

```text
engineering-work@4
+
software-change@7
+
golang@3
+
concurrency-bug@2
```

Governance conflicts MUST be deterministic. Recommended behavior:

1. combine monotonic requirements;
2. apply only explicit, authorized exemptions;
3. fail closed when constraints are incompatible;
4. expose the conflict as a visible diagnostic or obligation;
5. record the exact ontology versions and precedence used.

## 14.6 Temporal scope membership

Project membership, group membership, has-home changes, and governance links are all temporal links represented by events.

A historical project view must reflect the memberships and governance effective at the requested time.

## 14.7 Scopes are not automatically security boundaries

A project or group is an organizational and governance scope. Access control MAY use scopes, but the system MUST NOT assume that membership alone defines confidentiality or authorization. Security policy must be explicit.

---

## 14.8 v1 membership, views, and navigation (2026-08-19)

This subsection fixes the v1 semantics for projects and groups. It is the
normative contract behind the reference implementation and the CLI/SDK
surface (ticket #28).

### 14.8.1 Membership is conceptual, not people-based

Membership is exclusively entity-to-entity (tickets, projects, groups) and is
expressed as typed links. There is no user, team, role, owner, or permission
concept (14.7, N082).

- `has-home` (asserted) / `home-of` (inverse): the single numbering and
  default-governance project of a ticket.
- `contains` / `contained-by`: structural membership for navigation,
  reporting, and search.
- `governs` / `governed-by`: authority and policy inheritance.

### 14.8.2 Membership model

- A ticket SHOULD have at most one current `has-home` link (14.4, N080). A
  batch that would create a second current `has-home` on the same ticket is
  rejected with a provenance-bearing reason naming the existing assertion. A
  ticket with zero home projects is allowed.
- Legal `contains` targets in v1: project to ticket, project to project,
  group to project, group to group, group to ticket (direct containment).
- Multiple `contains` links are allowed: a ticket may appear in several
  projects; a project may belong to several groups (14.2).
- `new --project P` creates the ticket and asserts `has-home` to `project:P`
  in the same atomic batch. If `project:P` does not exist, the batch fails
  with actionable guidance.
- Markdown import with `--project P` behaves identically: the imported
  ticket, its content, and the `has-home` assertion land in one atomic batch;
  a missing target fails with the same guidance. Reimport never changes
  membership.
- Membership is links only; tickets never carry a `project` part.

### 14.8.3 Link target resolution and operations

- Link targets resolve at write time: asserting a link to a nonexistent
  project, group, ticket, part, or event is rejected with actionable
  guidance.
- Membership changes are the registry operations `assert-link` and
  `retract-link` (19), plus the Phase 4 membership transitions
  `join-scope` / `leave-scope` (ticket #74): a scope membership change
  realized as a `member-of` assertion or retraction (entity to project or
  group) that shares the link projection and evidence semantics (#66). The
  CLI `set .../links --add member-of:...` emits `assert-link`; the SDK
  `JoinScope`/`LeaveScope` emit the canonical `join-scope`/`leave-scope`
  operations; both produce identical visible membership.
- Link projection uses evidence semantics (9.8, #66): multiple assertions of
  the same triple coexist; the relation is visible while at least one
  assertion is active; retracting one assertion does not hide the relation
  while another remains; retracting all hides it.

### 14.8.4 Scope chain

Effective scope for a ticket is deterministic:

```text
has-home (via has-home link) -> its groups (canonical-ID order)
```

A `contains` project that is not the home project contributes to views and
search but not to schema resolution unless declared otherwise. The same
guardrail applies to direct group-to-ticket `contains`. Overlapping
memberships must not make resolution nondeterministic (14.5, N081).

### 14.8.5 Views and search

- Detail: `show project:<id>` and `group:<id>` render the entity (ref, title,
  status, recorded time), its parts (including schema declarations),
  membership links, references, history, and lineage.
- List: `show --kind project|group` lists scope entities and combines with
  search and status filters; `show --project P` / `--group G` filter tickets.
  `show --unscoped` selects tickets with neither project nor group view
  membership.
- Project view membership: tickets with an active asserted `contains` or
  `has-home` link from the project.
- Group view membership (union, canonical order): tickets with a direct
  asserted `contains` link from the group, plus tickets in projects the group
  `contains` or `governs` (one hop).
- These are view-membership relations only: direct group `contains` and
  project-derived group membership are unioned and deduplicated by canonical
  ticket identity. A ticket may therefore be visible through several paths
  while appearing once in the ticket list. Generic relations such as
  `related` and `supports` do not establish project or group view membership.
- `member-of` is a Phase 4 scope-membership relation and does not contribute
  to project or group ticket views in v1; that behavior remains the decision
  tracked by #84. Group-to-group and project-to-project links are not
  recursively traversed by these ticket views.
- Repeated filter values within one scope kind are unions. When both project
  and group filters are present, the project candidate set and group candidate
  set are intersected: `(projects union) AND (groups union)`. An empty scope
  selection is an all-tickets view. The explicit unscoped selection is
  mutually exclusive with project and group filters and matches only tickets
  absent from both candidate sets. Views reflect membership effective at the
  requested time (14.6), and tickets remain independent entities across
  projects (9.6).
- The SDK `ListFilter` and `SearchOptions` expose typed `Projects` and
  `Groups` collections and `Unscoped`; values are normalized by trimming,
  ignoring empty values, deduplicating, and sorting. Comma-separated parsing
  is an input-adapter behavior of the CLI and environment variables, not an
  SDK filter contract. The SDK exposes `CountTicketsFiltered` with the same
  semantics as listing.

### 14.8.6 Context is client-side scope only

Context is a client preference, not a model concept. `MISSIS_PROJECT` /
`MISSIS_GROUP` act as default scope filters when no explicit `--project` /
`--group` / `--unscoped` selector is given; explicit selectors override. The
`--unscoped` selector cannot be combined with project or group selectors. A TUI
may switch scope explicitly in-session. Context MUST contain only optional
project/group scope. It MUST NOT select a ticket, define an agent focus, derive
a task, or supply instructions. Legacy context and active-pointer Markdown is
not an authoritative input and is ignored by normal agent bootstrap. The core
and SDK remain stateless.

### 14.8.7 Changes and retraction

- Membership changes are link assertions and retractions only (14.6).
- Retracting a membership link removes it from current views; historical
  views at an earlier effective time still show it.
- Retracting the last current `has-home` assertion warns and is documented:
  the ticket enters the zero-home state, which is allowed (SHOULD, not MUST).
- No copy-per-project tickets (14.4): one canonical identity, one provenance
  history, multiple scoped views.

---

## 15. Large Markdown and incremental part updates

## 15.1 Equivalent input modes

Humans may prefer one complete document:

```bash
missis new --from bug.md
```

Agents may prefer small mutations:

```bash
missis set 184/hypothesis "Cancellation occurs after enqueue."
```

Both MUST produce the same internal concepts:

```text
Markdown input
    ↓ parse
parts + links + metadata
    ↓ validate
immutable events
```

Markdown is an input and output serialization, not the authoritative storage model.

## 15.2 Default heading mapping

Example input:

```markdown
# Retry after shutdown

## Problem

Worker can enqueue retries after shutdown.

## Hypothesis

Cancellation is checked after enqueue.

## Evidence

The defect has been reproduced through multiple methods.

### Race test

#### Command

`go test -race ./internal/worker/...`

#### Run 417

##### Result

Failed.

##### Error

Retry was enqueued after context cancellation.

## Plan

Move cancellation check before enqueue.
```

Default mapping:

```text
#184/title
#184/problem
#184/hypothesis
#184/evidence
#184/evidence/race-test
#184/evidence/race-test/command
#184/evidence/race-test/run-417
#184/evidence/race-test/run-417/result
#184/evidence/race-test/run-417/error
#184/plan
```

The same internal structure can be assembled incrementally:

```bash
missis set 184/evidence/race-test/command \
  'go test -race ./internal/worker/...'

missis set 184/evidence/race-test/run-417/result \
  'failed'
```

Recommended mapping rules:

- first H1 becomes the ticket title;
- H2 and deeper headings become recursive part paths;
- heading text is normalized to a stable slug;
- explicit IDs establish or retain stable part identity;
- duplicate headings receive deterministic suffixes or explicit identifiers;
- fenced code blocks are not parsed as headings;
- unheaded introductory content is preserved under `context` or an import-specific part;
- a heading may have both its own value and descendant headings;
- no unrecognized content is discarded.

## 15.3 Optional semantic annotations

Plain Markdown MUST work. Optional annotations MAY add precision:

```markdown
## Hypothesis {#retry-order type=hypothesis}

Cancellation occurs after enqueue.

Links:
- supports: #171/evidence/log-3
- affects: safedesign@abc123:worker/retry.go#retryLoop
```

The example creates a stable part identifier and typed links.

Front matter MAY specify:

```yaml
project: safedesign
types:
  - bug
  - concurrency-defect
ontology:
  - concurrency-bug@2
links:
  - relation: discovered-from
    target: "#171"
```

Annotations are optional convenience syntax, not a requirement for basic use.

## 15.4 Source-span provenance

Every imported part SHOULD retain its origin:

```text
artifact: investigation.md
span: lines 14-18
ingest event: @e182
processor: markdown-parts@2
```

Then:

```bash
missis show 184/hypothesis --why
```

may show:

```text
Cancellation is checked after enqueue.

source:
  investigation.md:14-18

imported by:
  @e182

processed by:
  markdown-parts@2
```

## 15.5 Atomicity

One Markdown import SHOULD be one atomic batch by default. If any required part fails validation, the entire import fails with a per-part diagnostic unless partial mode was explicitly requested.

## 15.6 Export and round-trip

```bash
missis show 184 --format markdown
```

SHOULD export a readable document. Optional hidden or visible identifiers SHOULD preserve part identity on re-import.

A round-trip need not reproduce exact whitespace, but it SHOULD preserve:

- title;
- part hierarchy;
- values;
- references;
- semantic annotations;
- source identities where appropriate.

## 15.7 Hierarchical identity during import and re-import

An importer SHOULD preserve explicit part IDs across export and re-import. This lets a heading rename or move without creating a different logical part.

The v0.1 Markdown transport encodes an explicit identity as an inert comment
immediately before the corresponding heading:

```markdown
<!-- missis-part {"id":"part:01K2MR7B8Q"} -->
### Race test
```

Goldmark identifies the heading and the importer associates only this exact
transport marker with it; ordinary HTML comments and marker-looking text in
fenced or indented code remain Markdown data. Export emits the marker for
known Parts, and re-import reuses the identity. A document title is represented
by the ticket rather than a child Part, so its descendant paths are normalized
when the title heading is removed during import.

Conceptually:

```markdown
### Race test {#part-01K2MR7B8Q}
```

If an explicit ID moves under a different heading, the importer proposes a `move-part` event rather than a delete-and-create pair.

Without an explicit ID, matching MAY use deterministic heuristics such as source artifact identity, previous source span, heading ancestry, content hash, and ontology type. Heuristic matches MUST be explainable and SHOULD require confirmation or conflict reporting when ambiguous.

One Markdown import SHOULD preserve hierarchy changes as one atomic batch so readers never observe a half-moved subtree.

## 15.8 Markdown transport safety and diagnostics

Missis Markdown transport metadata is an exact, full-line HTML comment. It is
not metadata hidden inside a code fence. Goldmark identifies fenced and
indented code blocks first; marker-looking text owned by those blocks remains
literal Markdown.

Part identity transport uses:

```markdown
<!-- missis-part {"id":"part:01K2MR7B8Q"} -->
## Evidence
```

The marker must contain a non-empty `id` and be attached to the next heading;
exporter-generated blank lines are allowed. No marker means ordinary Markdown
and the core assigns a new Part ID. An existing identity is reused only when
it resolves in the target projection. An unknown identity receives a new
core-assigned ID and an `identity_unresolved` diagnostic. A valid marker that
is not attached to a heading remains raw Markdown with an
`identity_unattached` diagnostic. Missing IDs, malformed JSON, and duplicate
identities reject the import atomically. Other HTML comments remain ordinary
Markdown.

Examples of identity diagnostics are:

```markdown
<!-- missis-part {"id":"part:unknown"} -->
## Imported heading

<!-- missis-part -->
## Missing identity

<!-- missis-part {"id":"part:duplicate"} -->
## First copy
<!-- missis-part {"id":"part:duplicate"} -->
## Second copy

<!-- missis-part {"id":} -->
## Invalid JSON
```

The unknown identity is syntactically valid but receives a new core ID and
an `identity_unresolved` diagnostic. Missing IDs, duplicate identities, and
invalid JSON fail atomically with line diagnostics; malformed metadata is
never silently discarded.

Inline sequence transport uses `missis-inline` comments. The complete inline
kind set is `markdown-text`, `code-ref`, `git-ref`, `artifact`, `image`,
`audio`, `video`, and `raw-markdown`. Marker order is semantic sequence order;
typed payloads contain only descriptors and references. Artifact bytes are
stored in the artifact backend, never in Markdown or event metadata.

For example:

```markdown
<!-- missis-inline {"ID":"inline-text-001","Kind":"markdown-text","Text":"A human explanation."} -->
<!-- missis-inline {"ID":"inline-image-001","Kind":"image","Data":{"kind":"image","uri":"artifact:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","media_type":"image/png"}} -->
<!-- missis-inline {"ID":"inline-audio-001","Kind":"audio","Data":{"kind":"audio","uri":"artifact:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","media_type":"audio/mpeg"}} -->
<!-- missis-inline {"ID":"inline-video-001","Kind":"video","Data":{"kind":"video","uri":"https://example.test/demo.mp4","media_type":"video/mp4"}} -->
<!-- missis-inline {"ID":"inline-artifact-001","Kind":"artifact","Data":{"Ref":{"Kind":"artifact","Entity":"artifact:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"MediaType":"application/octet-stream","Size":42}} -->
<!-- missis-inline {"ID":"inline-code-001","Kind":"code-ref","Data":{"Repository":"github.com/example/missis","Commit":"abc123","Path":"main.go"}} -->
<!-- missis-inline {"ID":"inline-git-001","Kind":"git-ref","Data":{"Repository":"github.com/example/missis","Commit":"abc123"}} -->
<!-- missis-inline {"ID":"inline-raw-001","Kind":"raw-markdown","Text":"![inert](https://example.test/image.png)"} -->
```

Malformed inline JSON, missing or duplicate IDs, unknown kinds, missing typed
data, and invalid descriptors produce explicit diagnostics or validation
errors. URLs, Git references, and raw Markdown media syntax are inert; no
renderer fetches or executes them. Normal UI views hide transport comments,
while raw Markdown export includes them for identity-preserving round trips.

The following remains literal code and is never promoted to transport data:

````markdown
```markdown
<!-- missis-part {"id":"part:literal-example"} -->
<!-- missis-inline {"ID":"literal-example","Kind":"image"} -->
```
````

Indented code is also literal:

```markdown
    <!-- missis-part {"id":"part:indented-literal"} -->
    <!-- missis-inline {"ID":"indented-literal","Kind":"image"} -->
```

Normal UI views hide transport comments; raw Markdown export keeps them for
identity-preserving round trips. Removing a valid Part marker causes an
identity-loss diagnostic, while changing its heading without removing the
marker preserves identity. Typed media, artifact, CodeRef, and GitRef data
should be edited through structured API/UI fields where available.

---

## 16. Search architecture

## 16.1 Parts are the primary search documents

Indexing only complete tickets produces oversized and semantically mixed documents. The primary search unit SHOULD be the part revision.

Examples:

```text
#184/problem
#184/hypothesis
#184/evidence/race-test
#184/code/retry-loop
#184/verification
```

A search document may contain:

```text
reference
canonical part ID
current or historical parent ID
ancestor IDs and names
part depth
breadcrumb
text or extracted text
part type
value kind
ontology types and versions
project and group scopes
recorded_at and effective_at
actor
links
provenance references
code references
Git references
current/superseded/retracted state
derived keywords
embedding metadata
```

Every nested part SHOULD be independently retrievable. Search results SHOULD display a breadcrumb:

```text
#184 › evidence › race-test › run-417 › error

Retry was enqueued after context cancellation.
```

A search projection MAY create aggregate documents for a parent subtree, but those aggregates are derived and rebuildable. They MUST NOT replace authoritative child values.

## 16.2 Search is a pipeline, not one engine

```text
query
  ↓
candidate generators
  ├── BM25 / lexical
  ├── vector similarity
  ├── metadata filters
  ├── graph and lineage traversal
  ├── symbol lookup
  ├── code-path lookup
  └── temporal lookup
  ↓
union and deduplication
  ↓
visibility, scope, ontology, and time filters
  ↓
reranker
  ↓
result projection and explanation
```

Conceptually:

```text
Search(q) =
  Rerank(
      BM25(q)
      ∪ Vector(q)
      ∪ Graph(q)
      ∪ Metadata(q)
      ∪ Symbol(q)
      ∪ Temporal(q)
  )
```

## 16.3 Supported retrieval modes

The system SHOULD be friendly to:

- exact reference lookup;
- exact phrase and token search;
- BM25;
- fuzzy lexical search;
- vector embeddings;
- hybrid lexical/vector retrieval;
- metadata filtering;
- temporal filtering;
- subtree, direct-child, ancestor, and bounded-descendant lookup;
- code path and symbol lookup;
- graph and lineage expansion;
- ontology-aware relation weighting;
- cross-encoder or LLM reranking;
- reciprocal-rank fusion;
- embedded in-process search;
- remote search services.

No authoritative data model may depend on one specific search engine.

## 16.4 Derived search artifacts

The following are derived, rebuildable artifacts:

```text
BM25 tokenization
inverted indexes
embeddings
embedding-model assignments
summaries
keywords
classifications
reranking scores
query caches
```

Invariant:

```text
Source events and part values are authoritative.
Search representations are disposable and rebuildable.
```

Embedding provenance SHOULD include:

```text
source part revision
model ID
model version or hash
chunking configuration
generation timestamp
processor invocation
```

## 16.5 Embedded and remote implementations

An MVP MAY use an embedded database and embedded search indexes. A larger deployment MAY use separate lexical and vector services.

The query layer should depend on interfaces such as:

```text
LexicalRetriever
VectorRetriever
GraphRetriever
MetadataRetriever
Reranker
```

not on one vendor-specific schema.

## 16.6 Temporal search

Current knowledge:

```bash
missis show --search "retry race"
```

Knowledge effective at a historical time:

```bash
missis show --search "retry race" --at 2026-07-01
```

Knowledge recorded during a period:

```bash
missis show --search "retry race" --since 7d
```

Full bitemporal search SHOULD support separate effective and known times.

Indexes MAY retain revisions or use event-time filters. The result MUST respect retraction and supersession under the selected time projection.

## 16.7 Lineage expansion

A lexical or vector hit may be expanded through links:

```text
BM25 hit: #184/hypothesis
    ↓ lineage expansion
#171/evidence/log-3
#184/code/retry-loop
commit:abc123
#184/verification/race
```

This is particularly useful when the exact query terms occur only in one part but the user needs causes, evidence, affected code, and verification.

## 16.8 Relation-aware retrieval

Ontologies MAY assign search meaning or weight to relations:

```text
weight(supports)     = high
weight(derived-from) = high
weight(related)      = low
```

Query intent may alter traversal:

```text
"why was this changed?"
  prefer caused-by, derived-from, motivated-by

"is this fix trustworthy?"
  prefer verified-by, supports, contradicts
```

## 16.9 Scope-aware search

```bash
missis show --search "retry" --project safedesign
missis show --search "authentication" --group security
missis show --search "race" \
  --project safedesign \
  --type concurrency-defect
```

Search across an overlapping group must return canonical entities, not duplicate copies.

## 16.10 Explainable ranking

`--explain` SHOULD show enough information to understand a result:

```text
#184/hypothesis
score: 0.91

candidate sources:
  BM25: rank 2, score 8.9
  vector: rank 1, cosine 0.87
  lineage: reached from #171/evidence/log-3

reranker:
  model: reranker-x@4
  score: 0.91
```

Scores are query-time observations, not authoritative facts.

## 16.11 Hierarchy-aware search

Search SHOULD support hierarchy-aware constraints and expansion:

```text
within subtree: #184/evidence/**
direct children of: #184/evidence
ancestor type: verification
maximum descendant depth: 3
include parent context: true
```

Candidate generation may operate on leaf or non-leaf parts. Reranking MAY include bounded parent, sibling, or ancestor context, but the result must identify the exact matched part.

Useful retrieval forms include:

```bash
missis show --search "context cancellation" \
  --within 184/evidence

missis show 184/evidence --search "failed" \
  --descendants
```

A hierarchy move changes path and ancestor metadata in current indexes, but canonical part identity and historical search revisions remain stable.

## 16.12 Search consistency

If indexes update asynchronously, the system SHOULD expose an index watermark or lag:

```text
ledger through: @e921
search index through: @e918
```

A caller MAY request a consistent read that waits for or bypasses the index by using direct projections. The CLI should never imply that an eventually updated index is the source of truth.

---

## 17. Views and projections

A **view** is a projection over the same authoritative events.

Recommended views:

```text
current
subtree
breadcrumb
history
temporal
temporal-diff
provenance
lineage
project
group
search
ontology
obligations
verification
effects
```

Command mapping:

```bash
missis show 184                    # current
missis show 184/evidence             # subtree rooted at a part
missis show 184/evidence --depth 2   # bounded subtree
missis show 184 --history             # history
missis show 184 --at ...           # temporal
missis show 184 --between ...      # temporal diff
missis show 184/status --why       # provenance
missis show 184 --lineage          # lineage
missis show --project safedesign   # project
missis show --group security       # group
missis show --search "..."        # search
missis show 184 --obligations      # ontology obligations
missis show 184 --verification     # verification
missis show 184 --effects          # observed effects
```

The system MUST avoid duplicating authoritative data for each view. Materialized projections are caches and indexes that can be regenerated.

---

## 18. Suggested data model

The following Go-like types are illustrative.

## 18.1 References

```go
type Ref struct {
    Kind   string   // ticket, part, event, project, group, run, code, git, artifact
    Entity string   // canonical immutable identity
    Path   []string // optional temporal human-readable alias, never canonical identity
}
```

## 18.2 Event

```go
type Event struct {
    ID       EventID
    Stream   Ref
    Sequence uint64
    BatchID  *BatchID

    Operation Operation
    Target    Ref
    Value     Value

    RecordedAt  time.Time
    EffectiveAt time.Time

    Actor ActorRef

    Sources    []SourceRef
    Inputs     []Ref
    Causes     []Ref
    Effects    []Effect
    Supersedes []EventID

    Ontologies []OntologyRef
    Invocation *InvocationRef

    PreviousHash string
    Hash         string
}
```

`PreviousHash` and `Hash` are optional if tamper evidence is not required in the first implementation.

## 18.3 Part and containment projections

```go
type Part struct {
    ID       PartID
    TicketID TicketID

    // Current projection only. Authoritative changes live in events.
    Name        string
    DisplayName string
    ParentID    *PartID

    // Nil means this part currently has children or metadata but no direct value.
    Value     *Value
    ValueKind string

    Types []string

    CreatedBy   EventID
    CurrentFrom EventID
    RetractedBy *EventID

    Sources []SourceRef
}
```

```go
type PartContainment struct {
    TicketID TicketID
    ChildID  PartID

    // Nil means the child is top-level directly beneath the ticket.
    ParentID *PartID

    AttachedBy  EventID
    DetachedBy  *EventID
    EffectiveAt time.Time
}
```

A `Part` and `PartContainment` are projections. Their authoritative changes are stored as events.

A path resolver derives a path from the part's temporal name and containment ancestry:

```go
type ResolvedPartPath struct {
    PartID     PartID
    TicketID   TicketID
    Segments   []string
    EffectiveAt time.Time
    KnownAt     time.Time
}
```

The current `ParentID` field is a convenience projection. It MUST be reconstructable from containment events.

## 18.4 Link projection

```go
type Link struct {
    ID       LinkID
    From     Ref
    Relation string
    To       Ref

    Origin string // asserted, inferred, processor, import

    CreatedBy   EventID
    RetractedBy *EventID
}
```

## 18.5 Code reference

```go
type CodeRef struct {
    Repository string
    Commit     string
    Path       string

    StartLine *int
    EndLine   *int
    StartByte *int
    EndByte   *int

    Symbol    *string
    Package   *string
    ASTNode   *string
    CFGNode   *string
    DFGNode   *string
    GraphNode *string
}
```

## 18.6 Git reference

```go
type GitRef struct {
    Repository string

    Commit *string
    Base   *string
    Head   *string
    Branch *string
    Tag    *string
    PR     *string
    Diff   *string
}
```

## 18.7 Effect

```go
type Effect struct {
    Kind string
    Ref  *Ref

    Before *Value
    After  *Value

    ObservedAt *time.Time
    Evidence   []Ref
}
```

## 18.8 Evidence and verification

```go
type Evidence struct {
    Ref       Ref
    Kind      string
    ClaimRefs []Ref
    Sources   []SourceRef
    ProducedBy Ref
}

type VerificationResult struct {
    Ref       Ref
    Claim     Ref
    Method    string
    Status    string // unverified, verified, failed, inconclusive, stale
    Evidence  []Ref
    Evaluator Ref
    Ontology  OntologyRef
}
```

## 18.9 Ontology reference

```go
type OntologyRef struct {
    ID      string
    Version string
    Hash    string
}
```

## 18.10 Processor invocation

```go
type ProcessorInvocation struct {
    ID          InvocationID
    Processor   string
    Version     string
    CodeHash    string
    ConfigHash  string

    Inputs      []Ref
    InputEvents []EventID
    Outputs     []Ref

    Capabilities []string

    StartedAt time.Time
    EndedAt   time.Time
    Status    string
    Diagnostic string
}
```

## 18.11 Search document

```go
type SearchDocument struct {
    Ref      Ref
    Revision EventID
    Text     string

    PartID    *PartID
    ParentID  *PartID
    Ancestors []PartID
    Breadcrumb []string
    Depth      int

    Types      []string
    Ontologies []OntologyRef
    Projects   []Ref
    Groups     []Ref

    RecordedAt  time.Time
    EffectiveAt time.Time

    Actor ActorRef

    Links    []LinkSummary
    CodeRefs []CodeRef
    GitRefs  []GitRef

    Current    bool
    Retracted  bool
    Superseded bool
}
```

---

## 19. Event operations

The CLI still exposes only `new`, `show`, and `set`, but the ledger may distinguish internal operations:

```text
create-entity
create-part
set-value
add-value
retract-value
rename-part
move-part
attach-child
detach-child
retract-subtree
restore-part
assert-link
retract-link
assign-ontology
remove-ontology
join-scope
leave-scope
observe-effect
attach-evidence
record-verification
supersede-event
```

Operations whose feature phases have not landed (`assign-ontology`,
`remove-ontology`, `join-scope`, `leave-scope`, `observe-effect`,
`attach-evidence`, `record-verification`) are declared projection-neutral
markers: they are accepted, validated, and visible in history/provenance, but
have no projection effect until their phase lands. The reference registry in
`internal/model/registry.go` is the executable definition; a new phase
registers new semantics rather than reinterpreting old events.

These operation types improve projection and validation while keeping the external command vocabulary stable.

---

## 20. Contracts and invariants

## 20.1 Event append preconditions

Before an event is accepted:

```text
Preconditions:
- target reference is syntactically valid;
- actor is identified;
- effective_at is valid or defaults to recorded_at;
- value conforms to its declared value kind;
- applicable ontology versions resolve deterministically;
- proposed operation passes validation;
- referenced entities either exist or the relation explicitly permits unresolved references;
- a proposed containment relation does not create a cycle;
- a proposed current path does not collide with another current part in the same ticket and time projection;
- recursive operations declare explicit scope, depth, and atomicity;
- processor-generated events identify their invocation;
- required capabilities were granted for any external effects.
```

## 20.2 Event append postconditions

After successful append:

```text
Postconditions:
- the event has a unique immutable ID;
- recorded_at is system assigned;
- per-stream sequence is monotonic;
- the event is atomically committed under the adapter's reported durability
  profile before success is returned;
- the current projection can be recomputed from events;
- provenance references are retained;
- projection and indexing work is either completed or durably queued;
- no earlier event was destructively changed.
```

## 20.3 System invariants

1. **Append-only history**

   ```text
   Accepted historical events are immutable.
   ```

2. **Reproducible projection**

   ```text
   Same event set + same ontology versions + same projection rules
   ⇒ same projected state
   ```

3. **No hidden plugin mutation**

   ```text
   Plugin-visible state change ⇒ accepted provenance-bearing event
   ```

4. **Explicit actor and time**

   Every accepted event has an actor, `recorded_at`, and `effective_at`.

5. **Stable code coordinate**

   A verification-critical code reference contains repository and immutable commit.

6. **Retraction preserves existence**

   Retracted information remains historically queryable.

7. **Current state is derived**

   The materialized current table is not authoritative.

8. **Search is rebuildable**

   Deleting every search index must not destroy ticket truth.

9. **Ontology version is explicit**

   A validation or verification judgment identifies the ontology version used.

10. **External effect is distinguishable from intention**

    A claimed action does not automatically prove an effect.

11. **Canonical identity is unique**

    Multiple project views of one ticket do not create multiple authoritative histories.

12. **Inverse relations are consistent**

    Declared inverse relations are derived consistently under the selected time projection.

13. **One recursive part type**

    ```text
    subpart = Part + temporal containment relation
    ```

    There is no separate authoritative `Subpart` entity.

14. **Containment is acyclic**

    ```text
    ∀p: ¬descendantOf(p, p)
    ```

15. **Stable part identity**

    A rename or move changes path projections, not canonical `PartID`.

16. **Temporal containment**

    Parent-child relationships and paths are reconstructable for historical effective and known times.

17. **No implicit hierarchy cascade**

    Parent and child values, types, permissions, statuses, verification states, retention rules, and visibility do not cascade unless an explicit ontology or policy says so.

18. **Part is not hidden work item**

    Independent coordination is represented by linked tickets rather than an implicit workflow on every descendant part.

## 20.4 Defensive validation

The kernel SHOULD use defensive validation at trust boundaries:

- CLI parsing;
- Markdown import;
- plugin output;
- external artifact ingestion;
- ontology loading;
- reference and historical-path resolution;
- containment cycle detection;
- subtree traversal limits;
- recursive retraction or move batches;
- effect receipt ingestion;
- search filter construction.

Internally, once an event batch has passed validation and entered the ledger, projection code may use stronger invariants and fail loudly on corruption.

---

## 21. Concurrency and transactional behavior

## 21.1 Optimistic concurrency

Two agents may update the same part concurrently. `missis set` SHOULD support an expected revision:

```bash
missis set 184/hypothesis "..." --if-current @e118
```

Precondition:

```text
current revision of target == @e118
```

If false, the write is rejected with the newer revision and a semantic diff.

## 21.2 Concurrent hierarchy mutations

A move, rename, attach, detach, or recursive operation SHOULD support expected hierarchy revisions:

```text
expected current parent
expected current name
expected subtree root revision
```

Two concurrent writes that create different children under one parent may commute when their resulting paths do not collide. Two writes that move or rename the same part, create the same path, or change overlapping subtrees require explicit conflict handling.

A subtree move or recursive retraction SHOULD commit atomically so no reader observes a partially transformed hierarchy.

## 21.3 Commutative additions

Appending distinct named parts or set-like values can often commute. The event model SHOULD avoid unnecessary conflicts for:

- adding independent evidence items;
- adding independent links;
- adding notes;
- adding code references.

Replacing the same scalar part should require explicit conflict handling or supersession.

## 21.4 Atomic batches

A batch containing parts, links, and metadata SHOULD commit atomically when they represent one user operation, one import, or one plugin result.

## 21.5 Idempotent clients

`new` and `set` SHOULD accept an idempotency key. A key is bound atomically to
one versioned request fingerprint, the accepted event IDs, and the result.

The version-1 fingerprint is SHA-256 over the domain-separated
framing:

```text
SHA256(
  UTF8("MISSIS-IDEMPOTENCY-REQUEST") || 0x00 || UTF8("v1") || 0x00 ||
  request_envelope_json
)
```

The outer envelope field order is `operation`, `actor`, `effective_at`,
`known_at`, `if_current`, `because`, `payload`; empty optional fields are
omitted. Operation-specific payload field order and JSON encoding are frozen
by the format-revision-3 implementation and compatibility fixture. This local
v1 encoding must not be claimed as a cross-language protocol until v3-alpha
publishes independent canonical vectors.

The envelope includes the operation, actor after documented defaulting,
caller payload, explicit effective/known times, expected-current precondition,
and causal reference. Ingestion fingerprints include the content-addressed
artifact digest and normalized media/capability fields. It excludes
authority-assigned event/ticket IDs, generated aliases, stream sequences,
recorded time, and omitted time defaults. Caller-supplied IDs and reference
text remain part of the payload. Equivalent references are not collapsed in
v1: a retry must use the same public request representation.

Repeating the same request and key MUST return the original result without
creating duplicate tickets or events. Reusing the key for a different
fingerprint MUST fail with `idempotency_mismatch` in conflict exit class 5 and
append nothing. The receipt and request fingerprint MUST commit in the same
transaction as the events.

Format-revision-2 receipts without a request fingerprint cannot be safely
reconstructed from their result or accepted events. Revision-3 migration MUST
move them out of the active receipt table into permanent
`idempotency_key_tombstones`; it MUST NOT invent a fingerprint. Guarded
high-level replay and key reuse fail closed and instruct the caller to use a
new key. Unguarded store-level lookup remains available for audit of the
stored result and event IDs.

Processing order is:

```text
fingerprint caller request before authority-assigned defaults
begin transaction
lookup key
  absent          -> validate and append
  same hash       -> return stored receipt without re-executing
  different hash  -> idempotency_mismatch, no append
  v2 tombstone    -> idempotency_mismatch with new-key guidance
commit events + request hash + receipt atomically
```

Replay precedes current-state validation: the original request may no longer
be valid against later state even though it already committed. The stored
receipt is returned; the operation is not executed again.

Required conformance cases:

| Case | Result |
| --- | --- |
| Same key, identical request | Original receipt/events replayed; event count unchanged. |
| Same key, changed operation/payload/time/precondition/content digest | `idempotency_mismatch`; event count unchanged. |
| Two concurrent different requests with one key | Exactly one may commit; the other mismatches. |
| Response lost after commit | Identical retry returns the committed receipt. |
| Migrated format-v2 tombstone | Guarded replay and reuse fail closed; evidence remains auditable and the key is never rebound. |
| Unknown/malformed fingerprint version | Fail closed; never treat it as a match. |

---

## 22. Error model and exit behavior

Recommended exit classes:

```text
0  success
2  invalid command or input
3  reference not found
4  validation or ontology failure
5  optimistic concurrency conflict
6  permission or capability denied
7  processor or hook failure
8  storage or integrity failure
9  search projection unavailable or stale beyond policy
```

JSON errors SHOULD be structured:

```json
{
  "error": "validation_failed",
  "target": "#184/status",
  "message": "cannot transition to done",
  "missing_obligations": [
    "verification/concurrency-fix"
  ],
  "ontology": "concurrency-bug@2"
}
```

Human errors should state:

1. what failed;
2. why;
3. which ontology or policy caused it;
4. which references are relevant;
5. the smallest valid next action.

---

## 23. Security and trust boundaries

## 23.1 Capability model

Plugins, agents, and hooks SHOULD receive explicit capabilities. Suggested capability namespace:

```text
ledger.read
ledger.propose
repo.read
repo.write
process.exec
process.test
filesystem.read
filesystem.write
network
git.commit
git.push
credential.use:<name>
search.read
search.rebuild
```

## 23.2 Default-deny effects

Effectful capabilities SHOULD be denied by default, especially:

- network;
- arbitrary shell;
- repository write;
- credentials;
- Git push;
- deployment;
- destructive file operations.

## 23.3 Provenance for permissions

An invocation record SHOULD state:

```text
requested capabilities
granted capabilities
denied capabilities
governing policy or ontology
```

## 23.4 Untrusted Markdown and plugin output

Markdown is data, not executable code. Embedded commands, HTML, links, or annotations must not cause effects unless a processor with explicit capabilities interprets them.

Plugin output MUST be validated as untrusted input before entering the ledger.

## 23.5 Secrets

Secret values SHOULD be referenced by opaque credential IDs rather than copied into ticket parts or event payloads. Logs, search indexes, and embeddings must not automatically ingest secret material.

## 23.6 Hierarchical policy inheritance

Containment alone MUST NOT imply authorization inheritance. A policy may explicitly declare descendant inheritance, but the evaluated rule, ontology or policy version, source part, and affected descendants must be explainable.

For example, confidential visibility MAY intentionally cascade:

```text
Confidential(parent)
∧ descendantOf(child, parent)
  → Confidential(child)
```

Without such a rule, parent and child authorization are evaluated independently. Recursive search and subtree export MUST check authorization for every returned part and must not leak hidden descendant counts, paths, embeddings, or snippets.

## 23.7 Search visibility

Search candidate generation and reranking MUST apply authorization filters. A vector similarity result must not reveal that a hidden part exists.

---

## 24. Persistence and projection architecture

## 24.1 Logical architecture

```text
                    append-only
                       ledger
                         │
       ┌─────────────────┼──────────────────┐
       │                 │                  │
       ▼                 ▼                  ▼
 current part tree   temporal views     provenance graph
       │                 │                  │
       └──────────┬──────┴──────────┬───────┘
                  ▼                 ▼
             scope views        search projections
                              /       |        \
                           BM25     vectors   graph
                                  
```

## 24.2 Suggested storage components

An embedded MVP may use:

- one transactional database for events and core projections;
- a content-addressed blob store for large Markdown and artifacts;
- an embedded lexical index;
- an optional embedded vector index;
- a durable outbox for post-processors and index updates.

A larger deployment may separate those components while preserving the same contracts.

## 24.3 Suggested relational projections

Possible tables or logical collections:

```text
events
batches
entities
part_current
part_revisions
part_containment_current
part_containment_revisions
part_path_aliases
link_current
link_revisions
scope_membership_current
ontology_assignments_current
obligations_current
verification_current
processor_invocations
artifacts
search_lexical
search_vectors
projection_watermarks
```

Only `events` and immutable artifact content are authoritative. The other structures are rebuildable projections.

## 24.4 Durable processor queue

After an event commits, post-processing work SHOULD be placed in a durable outbox in the same transaction. This prevents the state:

```text
event committed
but required hook notification lost
```

## 24.5 Large artifacts

Large Markdown imports, logs, test reports, traces, and binary artifacts SHOULD be content-addressed. Event payloads should store the artifact hash, media type, size, and selected source spans rather than duplicate large content.

---

## 25. Human-readable projections

## 25.1 Current ticket

```text
#184 Retry after shutdown

status:   doing
priority: high
project:  safedesign
types:    bug, concurrency-defect, go-code-change

problem
  Worker can enqueue retries after shutdown.

hypothesis
  Cancellation is checked after enqueue.

code
  [retry-loop]
  safedesign@91fa1c2:internal/worker/retry.go#retryLoop

obligations
  ✓ problem
  ✓ reproduction
  ✓ affected code
  ○ concurrency-fix verification
```

## 25.2 Timeline

```text
#184 Retry after shutdown

12:41  human/ravin
       created issue

12:45  human/ravin
       problem
       Worker occasionally retries after shutdown.

12:53  agent/codex/run-38
       hypothesis
       Cancellation check happens after enqueue.

       ← safedesign@91fa1c2:
         internal/worker/retry.go#retryLoop

13:02  process/test/run-772
       evidence/race-test
       Failure reproduced at iteration 417.

13:11  agent/codex/run-38
       code effect
       91fa1c2 → afe2821
       M internal/worker/retry.go

13:14  process/test/run-773
       verification
       10,000 race-test iterations passed.

13:15  agent/codex/run-38
       status
       doing → done
```

## 25.3 Why view

```text
blocked

set:
  2026-08-15T13:17:04+08:00

by:
  agent/codex/run-7

because:
  #171/status = doing

evidence:
  #184/evidence/build-12

event:
  @e114

validated under:
  bug@3
  engineering-work@4
```

## 25.4 References and backlinks

```text
Referenced by:

#219/problem
    blocked-by → #184

#227/evidence
    contradicts → #184/hypothesis

#241/code/2
    derived-from → #184/code/retry-loop
```

## 25.5 Subtree and breadcrumb view

```bash
missis show 184/evidence/race-test
```

```text
#184 › evidence › race-test
part: part:01K2MR7B8Q

summary
  Reproduces the shutdown retry defect under the race detector.

command
  go test -race ./internal/worker/...

environment
  linux/amd64, Go 1.x

run-417
  result
    failed

  stderr
    Retry was enqueued after context cancellation.
```

A temporal view MAY show the path that was valid at the selected time while also displaying the stable canonical part ID.

---

## 26. End-to-end scenarios

## 26.1 Human creates one large Markdown ticket

Input:

```bash
missis new --from retry-race.md
```

Pipeline:

```text
retry-race.md
  ↓ pre.ingest markdown-parts@2
parts and source spans
  ↓ ontology resolution
bug@3 + concurrency-bug@2 + go-code-change@7
  ↓ validation
accepted batch
  ↓ event append
immutable events
  ↓ post.part source resolver
resolved symbol links
  ↓ obligation derivation
reproduction and verification obligations
  ↓ search projection
BM25 and vector documents per part
```

Result:

```text
#184
├── problem
├── hypothesis
├── evidence/race-test
├── plan
└── code/retry-loop
```

Every part retains the original Markdown line span.

## 26.2 Agent performs a safe work loop

```bash
missis show --next --project safedesign --json
```

The ontology-derived next obligation is:

```json
{
  "ticket": "#184",
  "obligation": "verification/concurrency-fix",
  "acceptable_methods": [
    "race-test",
    "stress-reproduction",
    "model-check"
  ],
  "recommended_method": "race-test"
}
```

Agent claims explicit state:

```bash
missis set 184/status doing --if-current @e120
```

Agent records intended action:

```bash
missis set 184/intent \
  "Move cancellation check before enqueue."
```

An effectful processor modifies the repository under granted capabilities and records:

```text
input commit: 91fa1c2
action: modify retry.go
observed effect: commit afe2821 created
changed path: internal/worker/retry.go
```

A test processor records evidence:

```bash
missis set 184/evidence/race-test --from test-result.json
```

The ontology evaluates verification. Only then may:

```bash
missis set 184/status done
```

succeed.

## 26.3 Discovery is recorded after the actual incident

At 14:00, logs reveal an incident at 12:37:

```bash
missis set 184/evidence/crash \
  "Worker crashed" \
  --effective-at "2026-08-15T12:37:00+08:00"
```

The ledger assigns:

```text
recorded_at: 2026-08-15T14:00:00+08:00
effective_at: 2026-08-15T12:37:00+08:00
```

Queries:

```bash
missis show 184 --effective-at 13:00 --known-at 13:00
```

shows what was known then.

```bash
missis show 184 --effective-at 13:00 --known-at 15:00
```

shows what is now known about that earlier time.

## 26.4 Cross-project virtual connection

```bash
missis set safedesign#184/problem/links \
  --add same-origin:aici#73/problem
```

The parts remain separate. Search and lineage may cross the link:

```bash
missis show safedesign#184/problem --lineage --depth 3
```

No ticket duplication or project move occurs.

## 26.5 Ontology upgrade

Ticket #184 was valid under `bug@2`. The project upgrades to `bug@3`, which requires independent regression evidence.

The system appends or derives a re-evaluation result:

```text
historical evaluation:
  valid under bug@2

current evaluation:
  incomplete under bug@3

new obligation:
  independent-regression-evidence
```

History is not rewritten. The old verification remains historically true under its original ontology version.

## 26.6 Search with lineage and reranking

Query:

```bash
missis show --search "why did retry handling change" \
  --project safedesign \
  --lineage \
  --explain
```

Candidate generation:

```text
BM25 → #184/problem
vector → #184/hypothesis
symbol → #184/code/retry-loop
lineage expansion → @e103, commit afe2821, #184/evidence/race-test
```

Reranking presents the causal path rather than only a list of text chunks.

---

## 27. Recommended implementation sequence

## Phase 1: provenance kernel

Implement first:

- immutable event ledger;
- actor, `recorded_at`, and `effective_at`;
- stable ticket, part, and event identities;
- recursive parts with value, children, or both;
- temporal containment and historical path resolution;
- rename, move, attach, detach, and recursive retraction events;
- `missis new`, `missis show`, `missis set`;
- current part-tree projection;
- history projection;
- JSON output;
- retraction and supersession;
- optimistic concurrency for values and hierarchy mutations.

Provenance is not deferred to a later phase.

## Phase 2: references, code, Git, and links

Add:

- canonical references and human aliases;
- code references pinned to commits;
- Git references;
- typed links;
- inverse relations;
- backlinks;
- temporal links;
- lineage traversal.

## Phase 3: Markdown ingestion

Add:

- Markdown parser;
- recursive heading-to-part mapping;
- source spans;
- optional stable part IDs, annotations, and front matter;
- rename and move recognition during re-import;
- atomic hierarchy batch import;
- Markdown export with optional identity preservation.

## Phase 4: projects and groups

Add:

- project and group entities;
- home project;
- multiple memberships;
- `contains` versus `governs`;
- scoped aliases;
- project and group views;
- temporal membership.

## Phase 5: ontology, obligations, and verification

Add:

- versioned ontology loading;
- type assignment;
- validation rules;
- obligation derivation;
- completion contracts;
- verification method definitions;
- ontology composition;
- historical and current re-evaluation.

## Phase 6: processor and hook runtime

Add:

- processor manifests;
- deterministic hook phases;
- pure processor execution;
- effectful capability sandbox;
- invocation provenance;
- durable outbox;
- idempotency and cycle guards;
- diagnostics and retry policy.

## Phase 7: hybrid search

Add:

- per-part search documents;
- embedded BM25 or equivalent lexical search;
- metadata and time filters;
- vector embeddings;
- hybrid fusion;
- graph and lineage expansion;
- reranking;
- score explanation;
- index watermarks and rebuild.

## Phase 8: hardening and SafeDesign integration

Add:

- AST, CFG, DFG, and call-graph references;
- source impact processor;
- repository and tool effect receipts;
- tamper-evident event hashes where needed;
- policy-driven redaction;
- distributed projection workers;
- advanced verification ontologies;
- performance and fault-injection validation.

---

## 28. Minimum viable storage choice

For the simplest useful implementation, one process and one embedded transactional database are sufficient.

A practical MVP can provide:

```text
single executable
single database file
content-addressed artifact directory
embedded lexical search
optional embedded vectors
three subcommands
```

The design must still preserve interfaces that allow later replacement of:

- event database;
- blob store;
- lexical engine;
- vector engine;
- reranker;
- ontology evaluator;
- processor runtime.

Swappability should exist at component boundaries, not through user-visible command proliferation.

---

## 29. Acceptance criteria

### 29.1 Interface

- [ ] `missis new`, `missis show`, and `missis set` are the only required domain subcommands.
- [ ] A small allowlist of global operational flags may appear before a domain verb.
- [ ] Every agent operation can use explicit references and stable JSON.
- [ ] No operation depends on an implicit current ticket.
- [ ] `show` covers current views, lists, search, navigation, history, provenance, and lineage.
- [ ] `set` covers scalar updates, recursive parts, rename, move, attach, detach, subtree retraction, additions, links, code refs, Git refs, and corrections.

### 29.2 Parts and Markdown

- [ ] Tickets support arbitrary recursively nested addressable parts.
- [ ] `Part` is the only authoritative part entity; “subpart” is derived from temporal containment.
- [ ] Every part, including non-leaf parts, may have a value, children, or both.
- [ ] Every part has a stable canonical ID independent of its current path.
- [ ] Renaming or moving a part preserves its canonical identity and historical paths.
- [ ] Parent-child containment is temporal, provenance-bearing, and acyclic.
- [ ] A part has at most one current structural parent but may have arbitrary typed links.
- [ ] Parent value retraction does not silently retract descendants.
- [ ] Recursive removal, move, or restoration is explicit and atomic.
- [ ] No semantic or security property cascades through containment without an explicit ontology or policy rule.
- [ ] Information decomposition uses parts; independent work decomposition uses linked tickets.
- [ ] A large Markdown document can create or update a recursive part hierarchy.
- [ ] Incremental part updates produce the same internal model as Markdown import.
- [ ] Imported parts preserve source artifact, line-span, and parent-heading provenance.
- [ ] Explicit Markdown part IDs survive heading rename, move, export, and re-import.
- [ ] Unrecognized Markdown content is never silently discarded.
- [ ] Markdown export preserves part hierarchy, canonical identities where configured, and references.

### 29.3 Temporal behavior

- [ ] Every event has system-assigned `recorded_at` and explicit/defaulted `effective_at`.
- [ ] Current state is reproducible from events.
- [ ] Historical state can be queried by effective time.
- [ ] Historical knowledge can be queried by known time.
- [ ] Retraction removes a value from current state without erasing history.
- [ ] Corrections identify superseded events.
- [ ] Links and scope memberships are temporally queryable.

### 29.4 Provenance

- [ ] Every current value can be traced to an exact event.
- [ ] Events identify actors, sources, inputs, causes, and applicable ontology versions.
- [ ] Agent and processor runs have stable identities.
- [ ] Intention, action, observed effect, evidence, and verification are distinguishable.
- [ ] External effects are recorded when policy requires them.
- [ ] Plugin-produced facts identify the plugin version, code hash, inputs, and invocation.

### 29.5 Code and Git

- [ ] Verification-critical code refs contain repository, commit, and path.
- [ ] Code refs may select lines, symbols, packages, directories, and future graph nodes.
- [ ] Git refs support commits and commit ranges.
- [ ] Mutable branch refs also record the resolved commit.
- [ ] Code and Git references are searchable and linkable.

### 29.6 Links and lineage

- [ ] Any addressable reference can link to another addressable reference.
- [ ] Relation types are explicit.
- [ ] Declared inverse relations are consistent.
- [ ] Links may cross projects and groups without copying entities.
- [ ] Asserted and derived links are distinguishable.
- [ ] Lineage can traverse selected relations under a selected time projection.

### 29.7 Ontology and verification

- [ ] Ontologies are versioned and content-hashed.
- [ ] Validation and verification are separate operations.
- [ ] Ticket and part types may define required parts, required children, subtree shapes, legal links, obligations, and completion contracts.
- [ ] Hierarchy-based inheritance and cascades are explicit, versioned, deterministic, and explainable.
- [ ] Verification methods identify claims, evidence, evaluator, result, and ontology version.
- [ ] `done` can be blocked by unsatisfied ontology obligations.
- [ ] Ontology upgrades do not rewrite historical judgments.
- [ ] Composition is monotonic by default; exemptions are explicit and provenance-bearing.

### 29.8 Plugins and hooks

- [ ] Plugins can pre-process and post-process an exact part, child relation, descendant change, bounded subtree, or event phase.
- [ ] Subtree invocations record root, traversal policy, limits, and input revisions.
- [ ] Plugins return proposals, evidence, links, effects, or diagnostics rather than silently mutating storage.
- [ ] Processor pipelines and ordering are inspectable.
- [ ] Effectful plugins use capability grants and default-deny policy.
- [ ] Invocations are idempotent or safely deduplicated.
- [ ] Hook and descendant-trigger cycles are detected and stopped with depth, event-budget, and timeout controls.
- [ ] Failures are visible and do not erase accepted source events.

### 29.9 Projects and groups

- [ ] Projects and groups are first-class temporal entities.
- [ ] Projects may belong to multiple groups.
- [ ] Tickets have one canonical identity and may appear in multiple scopes.
- [ ] `contains` is distinct from `governs`.
- [ ] Effective ontology resolution is deterministic and explainable.
- [ ] Historical project and group views respect membership effective at that time.

### 29.10 Search

- [ ] Every nested part, not only whole tickets or leaf parts, can be an independent search document.
- [ ] Search documents may include stable part ID, parent, ancestors, depth, and breadcrumb.
- [ ] Search supports subtree, direct-child, ancestor, and bounded-descendant constraints.
- [ ] BM25 or equivalent lexical retrieval is supported.
- [ ] Vector retrieval can be added or replaced without changing authoritative storage.
- [ ] Metadata, scope, type, code, symbol, and temporal filters are supported.
- [ ] Hybrid candidate union and reranking are supported through interfaces.
- [ ] Lineage expansion can add connected evidence, code, commits, and verification.
- [ ] Embeddings and reranking scores are derived, versioned, and rebuildable.
- [ ] Search results can explain candidate sources and reranking.
- [ ] Search authorization prevents hidden-content leakage.
- [ ] Index lag or watermark is observable.

### 29.11 Reliability

- [ ] Event append and durable post-processing notification cannot be lost between separate commits.
- [ ] Event batches are atomic by default.
- [ ] Clients can use idempotency keys.
- [ ] Conflicting scalar and hierarchy updates support expected-revision checks.
- [ ] Subtree moves and recursive retractions are atomic and reject cycles or path collisions.
- [ ] Projection stores can be deleted and rebuilt from the ledger and immutable artifacts.

---

## 30. Final architecture

```text
                                    ┌──────────────────────┐
                                    │ Versioned Ontologies │
                                    │ types, rules, methods│
                                    └──────────┬───────────┘
                                               │
Input: title, Markdown, part update, link      │
                    │                          │
                    ▼                          ▼
            ┌────────────────┐       ┌───────────────────┐
            │ Pre-processors │──────►│ Validate proposal │
            └───────┬────────┘       └─────────┬─────────┘
                    │                           │
                    ▼                           ▼
             proposed recursive parts, links, events, evidence
                                │
                                ▼
                     ┌────────────────────┐
                     │ Immutable event log│
                     └──────────┬─────────┘
                                │
              ┌─────────────────┼─────────────────────┐
              │                 │                     │
              ▼                 ▼                     ▼
        current parts      temporal views       provenance graph
              │                 │                     │
              └─────────┬───────┴──────────┬──────────┘
                        │                  │
                        ▼                  ▼
                obligations/hooks      typed links
                        │                  │
                        ▼                  ▼
                 effects/evidence        lineage
                        │                  │
                        └──────────┬───────┘
                                   ▼
                         search projections
                 ┌─────────┬──────────┬──────────┐
                 ▼         ▼          ▼          ▼
               BM25      vectors    metadata    graph
                 └─────────┴────┬─────┴──────────┘
                                ▼
                             rerank
                                │
                                ▼
                         `missis show`
```

The public model remains small:

```text
new  = create identity and initial events
show = inspect, search, navigate, explain, and project
set  = propose a provenance-bearing change
```

The internal model remains coherent:

```text
Event  = immutable fact or change
Part   = addressable knowledge unit
Link   = typed virtual connection
Lineage = traversal over provenance-relevant links
Ontology = executable semantics and verification methodology
Processor = hookable, provenance-bearing transformation
Scope = project/group organization and governance
Projection = current, temporal, search, lineage, or verification view
```

The result is deliberately more capable than a conventional issue tracker while remaining simpler at the interface boundary. It is a temporal provenance ledger with a human-friendly and agent-friendly issue projection.

---

## 31. Guarantees and performance

Guarantees differ by layer; consumers must not assume a guarantee from one
layer at another. This appendix is the authoritative inventory for the store,
service, and SDK layers.

### 31.1 Terminology

| Term | Contract |
| --- | --- |
| Assertion | An `assert-link` event claiming that a relation holds between two refs. Visibility is derived from active evidence assertions. |
| Declaration | A part under a reserved `schema/` subtree mapping a key prefix to a value kind. It assigns meaning and is not a relation claim. |
| Precondition | A per-request write guard naming an expected current event. It is evaluated but never stored. |

Assertions and declarations are ledger records: bitemporal, retractable, and
provenance-bearing. Preconditions constrain a write and therefore cannot be
retracted.

### 31.2 Core and store

| Guarantee | Contract |
| --- | --- |
| Immutable events | Accepted events are never rewritten, deleted, or repaired in place. Corruption recovery restores a verified backup. |
| Stream sequences | Sequences are unique and strictly increasing per stream; a gap is an integrity incident. |
| Temporal winner | Latest effective time wins; recorded time breaks ties. |
| Event hashes | Canonical event encoding v1 and test vectors are defined, but revision-2/revision-3 live stores use `global-json-chain-v1`: SHA-256 over previous hash, newline, and implementation JSON. Ticket #57 owns adoption through a new named integrity epoch; accepted history is never silently rehashed. |
| Atomic batches | `AppendBatch` and `ApplyLinkBatch` commit all events in one transaction, including multi-stream batches. Failure writes nothing. |
| Idempotency | The key and versioned request fingerprint commit with the result/events. Same request replays; a different request is `idempotency_mismatch` and appends nothing. Migrated format-v2 tombstone keys fail closed for guarded replay and reuse. |
| Derived state | `tickets` and `parts_current` are rebuildable from the ledger with parity checks. |
| Preconditions | Part/entity and link-assertion expected-current-event mismatches are conflicts. Link preconditions use evidence semantics. |
| Retraction | Retraction is the only removal mechanism; it never erases accepted history. |

### 31.3 Service workflows

| Workflow | Guarantee |
| --- | --- |
| `NewTicket` | Creation is atomic. `--project P` asserts `has-home` in the same batch and a missing project fails before writing. |
| `NewEntity` | Existing canonical IDs are rejected; matching idempotency keys replay the original result. |
| Scope guard | Ticket creation rejects project/group tags; scope uses `--project` or explicit links. |
| Markdown import | Ticket, content, and project home are one atomic batch. Reimport never changes membership. |
| `SetLink` | Targets resolve at write time, endpoint and home uniqueness rules are enforced, and duplicate CLI assertions require `--allow-duplicate`. |
| `MoveLink` | Retract and assert are one guarded batch; only membership relations move. |
| `JoinScope` / `LeaveScope` | Membership assertion or retraction is one atomic batch and follows evidence semantics. |
| Import/reimport | Any violation rejects the complete batch. |

### 31.4 SDK

- The SDK is stateless and carries no hidden current context. Scope defaults
  are client preferences, never model state.
- Scope filters are typed collections. Values union within a scope kind and
  intersect between project and group kinds; unscoped selection cannot be
  combined with explicit scopes.
- Ticket views union and deduplicate direct and one-hop project/group
  membership paths by ticket ID.
- Reads and mutations use explicit, deterministically resolved refs.
- `pkg/missis` is the public facade; `internal/*` is not an external API.

### 31.5 Performance

`n` is ledger size, `s` is the complete history of the touched stream, and `p`
is the current projected part count of the touched ticket. These are measured
current bounds, not target guarantees. Correctness must remain unchanged while
derived-index work replaces linear reads.

| Path | Complexity | Notes |
| --- | --- | --- |
| Append | O(s log s + p) for a touched ticket stream | The current adapter refolds the touched stream and replaces its current part projection; #119 owns affected-key deltas. Work is independent of unrelated ledger history after #61. |
| Multi-stream batch | One transaction | Sequences remain per-stream. |
| Link-precondition evaluation | O(n) | Target of derived-index work. |
| Project/group listing | O(n) | Full-ledger scan until scope indexes land. |
| Reference existence | O(n) or O(stream) | Depends on reference kind. |
| Scope history | O(stream) | Loads one entity stream. |
| Schema resolution | O(n) per write | Target of declaration indexes. |

### 31.6 Local durability profile

The SQLite writer uses WAL with `synchronous=NORMAL`. Health reads and reports
the effective settings as `wal-normal`, including
`acknowledged_commit_power_loss_durable=false`.

This profile provides SQLite atomicity/consistency and ordinary process-crash
recovery. It does not promise that the newest acknowledged transaction
survives operating-system crash or host power loss, and it provides no
replication. A configurable WAL/FULL profile remains ticket #117 work and MUST
be benchmarked before any default change. Host-power-loss evidence is not
confirmed; safe testing uses a disposable VM and disposable virtual disk as
documented in `docs/durability-testing.md`, never destructive injection
against a developer machine's physical storage.

## 32. Change log

- 2026-08-27: froze v2; corrected the live hash and append-complexity
  descriptions; strengthened request-bound idempotency and its format-v2
  tombstone migration rule;
  documented the active WAL/NORMAL durability profile, and recorded store
  format revisions 3–5 for request binding, hashed identity, and strict
  durable external references. Broader event-store changes are proposed non-normatively
  in `event-store-v3-alpha.md` under ticket #118.
- 2026-08-19: added 9.8 (atomic link workflows and link-assertion
  preconditions), 14.8 (v1 project/group membership), the `has-home` /
  `home-of` vocabulary pair (9.2), and this guarantees appendix (31).
  Storage compatibility details remain in `docs/storage-compatibility.md`.

## 33. Artifact lifecycle and ordered containment hardening

The local artifact backend is content-addressed and durable before an
artifact reference becomes visible in the event ledger or SQLite artifact
index. `Put` copies into mode-0600 staging while computing SHA-256 and size,
flushes and closes the staging file, atomically publishes the digest path,
syncs its parent, atomically publishes metadata, and fully re-hashes the
published object before returning. Deduplication also fully hashes an existing
same-reference object; equal size is not accepted as equal content. The
application then commits the artifact index row and all events that first
reference it in one SQLite transaction. A failed database transaction may
leave only a safe unreferenced CAS object.

An accepted artifact-kind occurrence is derived by semantic decoding, never by
searching event JSON for an artifact-looking substring. Exact
`artifact:sha256:<digest>` values are managed CAS references. Existing accepted
history may contain named non-CAS source identities such as
`artifact:specs/report.md`; these preserve provenance but do not imply managed
bytes and MUST be reported as `unmanaged-reference` with exact event/field
locations. A malformed `artifact:sha256:` value remains an integrity error.
The inventory covers
every typed core `Ref` location in the accepted event envelope, effect
before/after values, typed descriptors/media/evidence/verification values,
and typed inline items. It records the exact event ID and field location for
each occurrence. The event ledger and immutable bytes are authoritative; the
SQLite `artifacts` table is rebuildable metadata.

`Stat` validates metadata and current size but does not hash content. `Open`
and `Put` deduplication perform a full digest verification before serving or
reusing an object. `missis-tools artifacts verify` takes exclusive database
and artifact leases, replays the semantic inventory, fully hashes the CAS, and
reports exact `healthy`, `unmanaged-reference`, `unreferenced-object`, `missing-index`,
`indexed-without-accepted-reference`, `missing-object`, `missing-metadata`,
`corrupt-object`, and `index-object-metadata-mismatch` states. Corruption
diagnostics MUST name the reference, expected and computed digest/size, and
every known accepted event/field occurrence. Missing or corrupt referenced
bytes MUST NOT be served; recovery requires that exact digest from a verified
backup or trusted exact copy. Event replay cannot reconstruct missing bytes.

When all accepted bytes verify but the artifact index cannot be reconciled,
`missis-tools artifacts rebuild-index-copy --destination NEW.db` MUST leave
the source unchanged, copy its exact accepted ledger/identity, rebuild only
the destination index from semantic replay and verified CAS metadata, verify
the destination, and publish it atomically. The output is an exact-identity
replacement candidate, not a second writable authority. GC MUST protect the
union of semantic accepted references and current index rows; it may collect
only objects in neither set after an explicit grace period and confirmation.

New stores use a per-store isolated root under the platform user-data
directory, namespaced by a hash of the immutable store identity. The
`MISSIS_ARTIFACT_STORE` environment variable is an explicit root override.
The exact publication, audit, recovery-copy, crash-state, writable-fork, and
remote-profile algorithms are recorded in
`docs/artifact-integrity-and-recovery.md`.

The pre-isolated layout is `<store-directory>/artifacts/`. A valid old root
is used with visible migration guidance when no isolated root exists. If both
roots exist, opening the store fails rather than choosing one silently.
`missis-tools artifacts migrate` validates and copies the old-layout CAS objects,
verifies every indexed artifact, and quarantines the old root as
`artifacts.legacy-<timestamp>`. The quarantine is retained for rollback and
is never removed automatically. `missis-tools artifacts gc` is an offline,
exclusive, dry-run-by-default operation that deletes only unindexed objects
older than an explicit grace period after confirmation.

Application clients and backup readers hold a shared store lease. Migration
and GC require an exclusive lease and fail with an explicit busy diagnostic
when active clients are present. `store.Open` and `store.OpenWithDiag` own a
shared database lease for the returned Store lifetime; maintenance callers
use `OpenWithLease`, and temporary backup databases use `OpenSnapshot`.
Advisory lock files may remain after a crash, but ownership is held by the
open descriptor and is released by the operating system. No stale PID cleanup
or timeout is used; lock failure is fail-closed and busy errors are returned
without indefinite waiting. The same lease abstraction protects artifact
roots, so restore takes exclusive leases for both its new database and its
artifact root before publication.

The rebuildable `parts_current` projection stores the core-assigned opaque
containment `order_key`. Migration `0006_ordered_parts` adds the column with
an empty pre-order-key default. Projection refresh, rebuild, and consistency checks
must match the key carried by the current containment event. Events without a
key retain deterministic stream-sequence/Part-ID ordering. Reordering emits a
new provenance-bearing event; clients do not calculate keys. Sparse numeric
keys are used first because ordinary insertion changes only the moved child.
If a decimal midpoint is exhausted, the core assigns fresh sparse keys in the
requested final sibling order and emits all changed containment events in one
atomic batch. It never silently appends to satisfy an unrepresentable
position, rewrites history, or patches only the projection.

Version-2 logical backups stage and validate the SQLite snapshot, manifest,
and embedded artifact sidecar before publication, then write
`backup.db.complete.json` last. The marker contains database and manifest
hashes, bundle version, and completion time. `missis-tools backup verify`
reports complete, database-only `legacy-v1`, incomplete, or corrupt state. Cleanup removes
only stale staging paths and explicitly incomplete bundles; valid published
backups are never removed automatically. Database-only and version-1 backups
remain readable.

`missis-tools backup [DESTINATION]` preserves an explicit destination. With no
destination it computes the live manifest and writes the content-addressed
bundle to `<project>/.missis-backups/<store-id>-<head-hash>.db`.
`MISSIS_BACKUP_DIR` overrides that directory; a relative override is resolved
against the project and an absolute override is used directly. An existing
derived bundle is skipped only after verification against the current store.
`missis-tools backup verify [BACKUP] --against-current` compares store ID,
head hash, schema version, and event count with the current store. Omitting the
backup path derives the content-addressed path and implies the comparison.
Remote upload uses the same resolver, so local creation and upload cannot
silently disagree about the default bundle.

Markdown remains raw data. Goldmark AST parsing protects fenced code, while
typed CodeRef, GitRef, media, and artifact child Parts provide explicit mixed
content. Markdown URLs and embedded media are inert and are never fetched or
executed by renderers. An explicit `inline-sequence` value is available when
one Part needs semantic inline order; its core-assigned item IDs and typed
items are rendered through `missis-inline` Markdown markers. Unmarked URLs,
images, audio, and video remain raw Markdown. Ordered mixed-content traversal
is verified across import, typed attachment, export/re-import, reorder,
close/reopen, and projection rebuild.

## 34. Store-format compatibility and verified releases

### 34.1 Store-format revision and compatibility corpus

Binary release identity and durable store compatibility are independent.
`store_format_revision` is one internal monotonically increasing integer;
revision 7 is the current format. Unmarked stores through migration 0005 are
implicit revision 1, unmarked stores through migration 0007 are implicit
revision 2, migration `0007_store_format_revision` records revision 2 in
`store_meta`, and migration `0008_idempotency_request_hash` adds the nullable
request fingerprint and records revision 3. Migration
`0009_store_identity_v1` adds exact hashed identity documents and receipts;
the Go identity step atomically records revision 4 because CSPRNG generation is
not delegated to SQL. Migration `0010_external_ref_v1` admits the strict
`external-ref-v1` durable value vocabulary and adds versioned format-migration
receipts; the explicit Go step preserves store identity and atomically records
revision 5 after its required backup.
Migration `0011_artifact_namespace_fork_v1` adds the independently inspectable
artifact-fork receipt index; the explicit Go step preserves store identity and
atomically records revision 6 after its required backup.
Migration `0012_canonical_event_epoch_v1` adds nullable exact accepted-record
bytes, codec/content identity, per-event integrity epochs, and transition
receipts; the explicit Go step preserves all format-6 event/hash bytes and
atomically records revision 7 after its required backup. Newly accepted rows
use `canonical-event-chain-v1`. A migrated store writes the first epoch
transition receipt atomically with its first post-migration event.

Opening an existing database MUST probe this revision read-only before WAL
configuration, migration, integrity verification, or projection repair.
Normal open accepts only the current revision. Known older revisions may move
forward only through `missis-tools store migrate plan/apply --to-format N`;
`apply` requires a pre-migration backup path. Unknown migrations and newer
revisions MUST fail closed with the found and supported revision range. The
gate records neither a binary hash nor a growing feature-name list: several
binary releases may correctly support the same store revision.

Changing a deliberately writable copy's identity is not a format migration.
It uses the independently versioned `missis-tools store fork
plan/apply/recover --to-identity-version 1` operation. Apply MUST require the
exact expected parent `--from-store-id` and a pre-change `--backup`; it MUST
atomically install a fresh identity document, new store ID, and lineage receipt
binding the parent document, fork head/count/integrity epoch, backup digest,
and artifact disposition. The plan reports artifact index rows, accepted-event
count, managed CAS occurrences, and unmanaged artifact-kind source occurrences
from semantic decoding rather than an event-JSON substring heuristic.

Any accepted managed reference without an artifact-index row MUST make the
plan ineligible with the exact missing-row count and direct the operator to
`artifacts rebuild-index-copy`. The fork MUST NOT repair its source database in
place.

Zero inventory uses `store-identity-fork-v1` and an empty child namespace.
Non-zero inventory uses `artifact-namespace-fork-v1` and
`store-identity-fork-v2`. The required copy set is the union of all index rows
and managed accepted references. Every source and child object MUST be fully
verified and physically copied without hard links. Unmanaged values MUST be
preserved exactly as `provenance-only-unmanaged-v1` and MUST NOT be interpreted
as paths or fetched. Valid CAS objects in neither set MUST be listed as
`excluded-unreferenced-v1` and omitted; invalid source CAS entries MUST stop the
fork.

The operation MUST persist one reusable child identity, a canonical sorted
manifest, and a completion marker before atomically publishing the child
namespace. Only then may SQLite commit the child identity, lineage receipt, and
matching manifest/marker index in one transaction. `fork inspect` MUST fully
verify and distinguish incomplete copy, prepared/uncommitted, complete,
identity mismatch, and integrity failure. `fork recover` MUST reuse the same
prepared identity and verified backup; it MUST NOT invent a second child or
adopt an unrelated destination. A read-only replica or ordinary backup
preserves identity and does not run this operation.

`internal/store/testdata/compatibility/revision-NNNN` is the immutable,
cross-platform compatibility corpus. Each retained revision contains a
deterministic SQLite fixture, manifest, and synthetic artifact CAS. The
manifest covers every registered operation/version, built-in durable value,
inline and reference kind, link relation, first-party ingestion plugin, and
the provenance and temporal shapes supported by that revision. Tests compare
logical rows, canonical event hashes, projections, plugin identities, and
artifact digests rather than raw SQLite bytes.

Adding a registered durable shape without fixture coverage fails ordinary
`go test ./...`. A compatible implementation change must reproduce the
existing logical snapshot. An incompatible encoding or interpretation change
requires an explicit revision increment and a new retained fixture directory;
the fixture tool never overwrites an accepted revision automatically.

Revision 2 records the ordered-containment and artifact-capable contract.
`v0.2.1` omitted `OrderKey` during typed event deserialization and therefore
recomputed a different hash for ordered events. It is explicitly unsupported
for revision-2 stores containing those events; the first compatible paired
release is `v0.2.2`.

Revision 3 binds every new idempotency receipt to a versioned request
fingerprint. Format-revision-2 rows move to permanent key tombstones because
their original caller request is unrecoverable; guarded replay and reuse fail
closed rather than guessing, while event IDs/result remain available for
audit. The active revision-3 table rejects a missing fingerprint. The
immutable revision-2 fixture remains retained.

Revision 4 installs the location-independent `eventstore-hash-v1` store
identity document and migration/fork lineage receipts. Revision 5 preserves
that identity while admitting strictly decoded `external-ref-v1` values.
Unknown fields—including paths, URLs, credentials, and embedded locators—are
rejected before append. Revision 6 preserves those semantics while adding the
artifact namespace fork manifest/marker/receipt index. The immutable
revision-6 fixture is the current generation target; revisions 2–5 remain
retained migration/conformance input.

### 34.2 Release identity and paired update

Stable Git tags remain SemVer. Operator-triggered publication requires an
explicit new stable version and rejects an existing, older, prerelease, or
non-SemVer value; a format-changing compatibility boundary uses an explicitly
reviewed version rather than silently selecting the next patch. The workflow
builds `missis` and `missis-tools` from the same full Git commit. Both
binaries report release version, commit, display identity
`vX.Y.Z+g<short-sha>`, and supported store-format revision. Development builds
report `dev+g<short-sha>` and dirty state when available.

Every platform release is one archive containing both binaries. Its HTTPS
release manifest binds release identity, store revision, platform,
architecture, archive size and SHA-256, and each binary SHA-256. The installer
and self-updater verify the archive and execute both staged binaries only for
identity inspection after checksum validation. Unsupported platforms,
malformed manifests, oversized archives, traversal entries, split installs,
mismatched identities, development/dirty builds, and downgrades fail closed.

The pair is staged before either live binary is replaced. A local update
journal and previous binaries permit rollback or completion on the next
invocation; the installation manifest is written last. POSIX replaces the
pair directly after verification. Windows starts a verified staged helper,
waits for the running executable to exit, then applies the same journaled
replacement. Release installation writes the same paired manifest. Independent
cryptographic signing is deferred; SHA-256 verification currently relies on
the GitHub HTTPS release trust boundary.

When a release changes the physical store format, paired replacement and store
migration form one recoverable rollout, not two unrelated operator actions and
not one claimed atomic transaction. The release manifest MUST declare the
normal-open format, the exact sorted source revisions its maintenance tool can
migrate, and a digest of that migration set. The target pair MUST be staged and
verified before any store mutation. An exclusive store lease then covers
re-planning, verified backup, exact-version migration, staged-pair
verification, paired activation, and final generation inspection.

The rollout journal MUST bind the previous pair and installation manifest to
the pre-migration database/artifact backup, and bind the staged pair to the
target migration receipt. Recovery MUST reach either the complete previous
generation or the complete target generation. It MUST reject split pairs, old
writers against the new store, new normal writers against the old store,
unverified staging, and receipt/backup/generation mismatches. Installing a
global pair MUST NOT silently discover or migrate project stores; every store
is an explicit rollout target.

### 34.3 Clean project setup

`missis --setup --project DIR` is the only project initialization operation.
It is a global operational flag, not a fourth domain command. A fresh setup
validates an in-project store before atomically publishing the `.missis`
marker. Repeating setup is idempotent and preserves an existing marker, store,
ledger, legacy metadata, and optional agent instructions.

`--setup --check` is read-only: it does not create or migrate a store, configure
WAL, repair projections, or write a marker. Both modes fail closed when the
binary pair, marker path, store format, integrity, or explicit project/group
scope cannot be confirmed. Stable builds require a verified paired installation.
An unpaired development pair is usable only with explicit
`--allow-development`, and the result states that release installation is not
confirmed.

The canonical clean bootstrap is a pinned invocation of
`tools/paired-install@<stable-tag>` with `--project DIR`. The installer derives
the same stable tag from its module identity, verifies and installs both
binaries, and invokes the installed `missis --setup` by absolute path. It never
selects `latest` implicitly. Setup itself performs no network lookup.
