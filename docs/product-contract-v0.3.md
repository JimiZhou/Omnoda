# Omnoda v0.3 product contract

Omnoda is a personal control system the operator owns. It runs a goal across devices the operator has paired. It is not a model gateway, a replacement harness, or a program that copies itself onto machines the operator did not enroll.

## Two modes, one store

Seed mode is a loopback process with a local node, a capability registry, and SQLite. It does not require Codex, Claude Code, OpenCode, Node.js, Docker, or Redis. The first task can call the built-in system-info capability. A model is optional. Without a key, the API returns `no_model` and does not write a fake answer.

Orchestrator mode uses the same conversations, tasks, approvals, and capability records. Pairing another device adds a node. It does not reset the seed database.

## Objects

A conversation stores messages. A task stores the goal and lifecycle. A run stores the node, adapter, and result of one execution. An event has a monotonic sequence. These are separate rows.

## Not in this version

No public relay, no cloud worker, no app store, no wake word, no full-duplex voice, no extra harness, no automatic install onto a machine that has not presented an enrollment code.
