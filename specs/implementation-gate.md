# Implementation gate

This gate answers one question before implementation work begins:

> Are the data model and testsuite aligned enough with the specification to
> start implementing without immediately drifting?

Passing this gate does not make the specification, data model, or testsuite
immutable. It means they are mutually traceable. If any one changes, the gate
must be run again and the other two brought back into alignment.

## Canonical basis

`specs/missues-issue-specification.v2.md` is the canonical specification. The
v1 file is historical and must not be used as a basis for new work.

Normative strength is taken from the specification's own language:

- `MUST` / `MUST NOT` items gate readiness.
- `SHOULD` items are desirable but do not block a first implementation slice.
- `MAY` items are optional and only become gates when explicitly adopted.

## Alignment rule

```text
spec item -> data model -> test
```

Every implementation-facing requirement must have:

1. an identifier in the canonical specification;
2. a concrete representation or rule in the data model;
3. at least one test that proves the represented behavior.

An item is considered **open** if any of those three links is missing.

## Readiness criteria

The gate is green only when every `MUST`-level item in the selected phase
passes. The first selected phase is the provenance kernel from the
specification's Phase 1, extended with the recursive part model.

### Phase 1 data model sufficiency

The data model must define, without relying on prose alone:

- `EventID`, `PartID`, `LinkID`, and the canonical reference forms;
- `ActorRef` and `SourceRef`;
- `Value`, including scalar, Markdown, structured, reference, and retracted
  representations;
- event append inputs and outputs;
- event operation semantics for create, set, retract, rename, move, attach,
  detach, subtree retraction, restore, and supersede;
- current projection;
- valid-time projection;
- known-time projection;
- bitemporal projection;
- recursive containment and path resolution;
- acyclicity and path-collision rules.

### Phase 1 testsuite sufficiency

The testsuite must be runnable from a clean checkout and must cover:

- `issue new`, `issue show`, and `issue set` through the public interface;
- stable JSON and exit-code behavior from section 22;
- current, historical, and bitemporal views;
- stable part identity across rename and move;
- temporal containment and historical paths;
- retraction without history deletion;
- supersession;
- append-only history;
- reproducible projection;
- single structural parent;
- containment acyclicity;
- path uniqueness;
- one positive and one negative case for each above invariant;
- at least one concurrent scalar update and one concurrent hierarchy mutation.

The public-facing suite must be black-box first so the same contract can run
against another implementation. Gray-box probes are allowed only where a
black-box result cannot distinguish correct behavior from a lookalike. A
gray-box test must state the core behavior it protects and the observable
reason the behavior cannot be proven through the public interface alone.

## Gate output

Running the gate produces a traceability table:

```text
spec item | strength | data model | test id | result
```

Rows may pass, fail, or be marked deferred. A deferred `MUST` item is a
blocker and must be either implemented or moved out of the selected phase by
an explicit specification change.

## Decision outcomes

```text
all MUST rows pass       -> implementation may start
one or more MUST fail    -> fix testsuite/data model/spec alignment first
SHOULD rows remain open  -> allowed, but listed as follow-up issues
```

## Change control

When any of the specification, data model, or testsuite changes:

1. update the changed artifact;
2. update the traceability table;
3. rerun the gate;
4. record unresolved gaps in `issues/` before implementation continues.

The three artifacts may evolve, but they are not allowed to pass through an
implementation checkpoint while out of alignment.
