# Provenance-First Temporal Issue Kernel

## Unified specification for a three-command, agent-friendly and human-friendly issue system

**Status:** Design specification  
**Primary interface:** `issue new`, `issue show`, `issue set`  
**Primary storage model:** immutable event ledger  
**Primary content unit:** recursive addressable part  
**Primary connection unit:** typed link  
**Primary semantic layer:** versioned ontology  
**Primary extension mechanism:** provenance-bearing processors and hooks  
**Revision:** 2026-08-15 — recursive part hierarchy and temporal containment made explicit

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

## 1. Executive summary

This system presents itself as a very small issue tracker, but internally acts as a provenance-aware workflow and knowledge kernel.

The public command vocabulary is deliberately limited to three domain verbs,
plus a small allowlist of global operational flags:

```text
issue new
issue show
issue set
```

Global operational flags:

```text
issue --version
issue --help
issue --self-update-check
issue --self-update
issue --init
issue --start
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
issue new
issue show
issue set
```

The implementation MUST NOT require separate top-level subcommands such as:

```text
list get find search next queue inbox close reopen assign comment block prioritize
```

Those behaviors are forms of `show` or `set`.

### 5.1.1 Global operational flags

The binary MAY accept a small allowlist of global flags before a domain verb:

```text
issue --version
issue --help
issue --self-update-check
issue --self-update
```

Global flags are process-level maintenance or inspection operations. They do
not create new domain subcommands, and they MUST NOT become a mechanism for
general ticket operations.

The allowlist MAY grow only for additional process-level maintenance or
inspection flags. It MUST NOT grow for flags that introduce a new command
subpath, mutate ticket state, or start a domain workflow. Those behaviors MUST
still be expressed through `issue new`, `issue show`, or `issue set`.

## 5.2 General command grammar

```text
issue new  [title] [input-options] [metadata-options]
issue show [reference] [view-options] [query-options] [format-options]
issue set  <reference> [value] [mutation-options] [provenance-options]
```

All commands SHOULD support:

```text
--json
--format text|json|markdown
--actor <actor-ref>
--effective-at <timestamp>
```

The system MUST assign `recorded_at` itself. A caller MUST NOT be able to forge transaction time through the normal interface.

## 5.3 No implicit current ticket

This is invalid as the only protocol:

```text
issue next
issue claim
issue finish
```

because the later commands may depend on hidden terminal state.

The preferred agent flow carries the ticket ID explicitly:

```bash
issue show --next --json
issue set 184/status doing
# perform work
issue set 184/evidence/test-run --from result.json
issue set 184/status done
```

Invariant:

```text
Every invocation must be interpretable without prior terminal state.
```

## 5.4 `issue new`

Create from a title:

```bash
issue new "Fix retry race in worker pool"
```

Create with metadata:

```bash
issue new "Fix retry race" \
  --project safedesign \
  --type bug \
  --type concurrency-defect \
  --priority high \
  --tag concurrency
```

Create from Markdown:

```bash
issue new --from bug.md
```

Create from standard input:

```bash
cat bug.md | issue new --stdin
```

Create another entity while preserving the same three-command vocabulary:

```bash
issue new --kind project --id safedesign "SafeDesign"
issue new --kind group --id engineering "Engineering"
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

## 5.5 `issue show`

With no reference, `show` presents the most useful current view:

```bash
issue show
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
issue show 184
```

Show one part:

```bash
issue show 184/hypothesis
```

Show one exact event:

```bash
issue show @e114
```

Filter or navigate:

```bash
issue show --status open
issue show --tag parser
issue show --blocked
issue show --next
issue show --project safedesign
issue show --group security
issue show --type concurrency-defect
issue show --search "retry race"
```

Temporal and provenance views:

```bash
issue show 184 --history
issue show 184 --at "2026-08-15T13:00:00+08:00"
issue show 184 --effective-at "2026-08-15T13:00:00+08:00" \
               --known-at "2026-08-15T14:00:00+08:00"
issue show 184 --since "2026-08-15T12:00:00+08:00"
issue show 184 --between "13:00..13:15"
issue show 184/status --why
issue show 184 --effects
issue show 184 --references
issue show 184 --lineage
```

Search views:

```bash
issue show --search "retry race" --project safedesign
issue show --search "authentication" --group security
issue show --search "race" --type concurrency-defect --since 7d
issue show --search "retry" --at "2026-07-01"
issue show --search "why was the worker changed" --lineage --explain
```

## 5.6 `issue set`

Set a scalar part:

```bash
issue set 184/status doing
issue set 184/priority high
issue set 184/problem "Worker retries after shutdown."
```

Set a nested or deeply nested part:

```bash
issue set 184/evidence/race-test \
  "go test -race failed at iteration 417"

issue set 184/evidence/race-test/run-417/stderr \
  "Retry was enqueued after context cancellation."
```

Rename or move a part while preserving its canonical identity:

```bash
issue set part:01K2MR7B8Q --name race-detector
issue set part:01K2MR7B8Q --parent 184/verification
```

Retract a complete subtree explicitly:

```bash
issue set 184/evidence/race-test --retract --recursive \
  --reason "Imported under the wrong ticket."
```

Append instead of replace:

```bash
issue set 184/notes --add "Observed on Linux arm64."
```

Set a blocked state with a reason:

```bash
issue set 184/status blocked \
  --reason "Waiting for #171"
```

Add a relationship:

```bash
issue set 219/links --add blocked-by:#184
issue set 220/links --add caused-by:#184
issue set 221/links --add duplicates:#184
```

Add a code reference:

```bash
issue set 184/code --add \
  --repo safedesign \
  --commit 9bd781a82b \
  --path internal/worker/retry.go \
  --lines 118:147
```

Add a symbol reference:

```bash
issue set 184/code --add \
  --repo safedesign \
  --commit 9bd781a82b \
  --path internal/worker/queue.go \
  --symbol Queue.Enqueue
```

Add a Git range:

```bash
issue set 184/git --add \
  --repo https://github.com/acme/safedesign.git \
  --range 813ac22..9bd781a
```

Import or merge Markdown into an existing ticket:

```bash
issue set 184 --from investigation.md
```

Retract rather than delete:

```bash
issue set 184/hypothesis --retract \
  --reason "Contradicted by #184/evidence/test-7"
```

Correct a prior exact event:

```bash
issue set 184/hypothesis \
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
issue set 184/experiment-1 \
  "Run worker tests 1000 times with the race detector."

issue set 184/result-1 \
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
issue set 184/evidence/race-test/command \
  'go test -race ./internal/worker/...'

issue set 184/evidence/race-test/run-417/result \
  'failed'

issue set 184/evidence/race-test/run-417/stderr \
  'Retry was enqueued after context cancellation.'
```

`issue show 184/evidence` returns the subtree without requiring a separate list or tree command.

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
issue show 184 --at "2026-08-15 14:00"
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
issue set 184/evidence --retract
```

By default, this retracts only the value stored directly on `#184/evidence`. Descendants remain current:

```text
#184/evidence/race-test
#184/evidence/production-log
```

Removing or detaching an entire subtree must be explicit:

```bash
issue set 184/evidence --retract --recursive
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
issue show 219 --at 13:45
```

may show the blocker, while:

```bash
issue show 219 --at 15:00
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
issue show #184/hypothesis --lineage \
  --direction both \
  --depth 4 \
  --relations derived-from,supports,contradicts,verified-by
```

---

## 10. Immutable event ledger and temporal model

## 10.1 `set` never destructively overwrites truth

Command:

```bash
issue set 184/status doing
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
issue show 184 --between "13:00..13:15"
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

`issue show --next` may choose the highest-priority unsatisfied obligation.

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
issue set 184/status done
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
- Membership is links only; tickets never carry a `project` part.

### 14.8.3 Link target resolution and operations

- Link targets resolve at write time: asserting a link to a nonexistent
  project, group, ticket, part, or event is rejected with actionable
  guidance.
- Membership changes are the registry operations `assert-link` and
  `retract-link` (19). `join-scope` and `leave-scope` remain
  projection-neutral marker operations until Phase 4 defines their
  semantics.
- v1 link projection is set-semantics: one active triple per
  (from, relation, to); multiple assertions of the same triple collapse, and
  retracting the last assertion hides the relation. Multi-assertion
  coexistence and per-assertion provenance are a separate deliverable.

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
- Project view membership: tickets with an active asserted `contains` or
  `has-home` link from the project.
- Group view membership (union, canonical order): tickets with a direct
  asserted `contains` link from the group, plus tickets in projects the group
  `contains` or `governs` (one hop).
- Repeated filter values are unions; views reflect membership effective at
  the requested time (14.6). Tickets remain independent entities across
  projects (9.6).

### 14.8.6 Context is client-side

Context is a client preference, not a model concept. `MISSIS_PROJECT` /
`MISSIS_GROUP` act as default scope filters when no explicit `--project` /
`--group` flag is given; explicit flags override. A TUI may switch context
explicitly in-session. The active pointer file is never written implicitly by
the command surface. The core and SDK remain stateless.

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
issue new --from bug.md
```

Agents may prefer small mutations:

```bash
issue set 184/hypothesis "Cancellation occurs after enqueue."
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
issue set 184/evidence/race-test/command \
  'go test -race ./internal/worker/...'

issue set 184/evidence/race-test/run-417/result \
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
issue show 184/hypothesis --why
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
issue show 184 --format markdown
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

Conceptually:

```markdown
### Race test {#part-01K2MR7B8Q}
```

If an explicit ID moves under a different heading, the importer proposes a `move-part` event rather than a delete-and-create pair.

Without an explicit ID, matching MAY use deterministic heuristics such as source artifact identity, previous source span, heading ancestry, content hash, and ontology type. Heuristic matches MUST be explainable and SHOULD require confirmation or conflict reporting when ambiguous.

One Markdown import SHOULD preserve hierarchy changes as one atomic batch so readers never observe a half-moved subtree.

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
issue show --search "retry race"
```

Knowledge effective at a historical time:

```bash
issue show --search "retry race" --at 2026-07-01
```

Knowledge recorded during a period:

```bash
issue show --search "retry race" --since 7d
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
issue show --search "retry" --project safedesign
issue show --search "authentication" --group security
issue show --search "race" \
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
issue show --search "context cancellation" \
  --within 184/evidence

issue show 184/evidence --search "failed" \
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
issue show 184                    # current
issue show 184/evidence             # subtree rooted at a part
issue show 184/evidence --depth 2   # bounded subtree
issue show 184 --history             # history
issue show 184 --at ...           # temporal
issue show 184 --between ...      # temporal diff
issue show 184/status --why       # provenance
issue show 184 --lineage          # lineage
issue show --project safedesign   # project
issue show --group security       # group
issue show --search "..."        # search
issue show 184 --obligations      # ontology obligations
issue show 184 --verification     # verification
issue show 184 --effects          # observed effects
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
- the event is durable before success is returned;
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

Two agents may update the same part concurrently. `issue set` SHOULD support an expected revision:

```bash
issue set 184/hypothesis "..." --if-current @e118
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

`new` and `set` SHOULD accept an idempotency key. Repeating the same request with the same key must return the same result or a clear conflict, not create duplicate tickets or duplicate events.

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
issue show 184/evidence/race-test
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
issue new --from retry-race.md
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
issue show --next --project safedesign --json
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
issue set 184/status doing --if-current @e120
```

Agent records intended action:

```bash
issue set 184/intent \
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
issue set 184/evidence/race-test --from test-result.json
```

The ontology evaluates verification. Only then may:

```bash
issue set 184/status done
```

succeed.

## 26.3 Discovery is recorded after the actual incident

At 14:00, logs reveal an incident at 12:37:

```bash
issue set 184/evidence/crash \
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
issue show 184 --effective-at 13:00 --known-at 13:00
```

shows what was known then.

```bash
issue show 184 --effective-at 13:00 --known-at 15:00
```

shows what is now known about that earlier time.

## 26.4 Cross-project virtual connection

```bash
issue set safedesign#184/problem/links \
  --add same-origin:aici#73/problem
```

The parts remain separate. Search and lineage may cross the link:

```bash
issue show safedesign#184/problem --lineage --depth 3
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
issue show --search "why did retry handling change" \
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
- `issue new`, `issue show`, `issue set`;
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

- [ ] `issue new`, `issue show`, and `issue set` are the only required domain subcommands.
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
                         `issue show`
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
