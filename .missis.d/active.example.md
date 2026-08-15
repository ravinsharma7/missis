# Active Session

This is a short-lived agent pointer. It is not authoritative domain data.

```text
store: .missis-store/missis.db
project: none
group: none
focus: SDK orchestration refactor
ticket: #21
```

Rules:

- Prefer missis refs over free-text descriptions.
- Do not duplicate authoritative ticket content here.
- Update this file only when the active project, group, or ticket focus changes.
- `project:` and `group:` values are canonical IDs, not display titles.
- Commands read this file but do not silently write it. Change it explicitly.
