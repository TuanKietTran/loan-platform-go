# OpenSpec agent instructions

This repository follows the [OpenSpec](https://github.com/Fission-AI/OpenSpec) layout.

- `openspec/specs/<capability>/spec.md` is the current behavior. Requirements use `### Requirement:` and `#### Scenario:` with **GIVEN**, **WHEN**, **THEN**, and **AND**.
- `design.md` next to a spec is how the service is built. It is not the contract.
- Proposed work lives in `openspec/changes/<change-id>/` (`proposal.md`, `tasks.md`, and spec deltas under `specs/`).
- Delta files use `## ADDED Requirements`, `## MODIFIED Requirements`, `## REMOVED Requirements`, and `## RENAMED Requirements`. A modified requirement is copied in full under its existing header.
- At every checkpoint, move the finished change to `openspec/changes/archive/YYYY-MM-DD-<change-id>/`, update `openspec/specs/` so it matches the code, and add a row to the checkpoint table in `openspec/project.md`.
- Do not write a current requirement for a command that is only named in the README. Those stay in the out-of-scope list until they ship.
