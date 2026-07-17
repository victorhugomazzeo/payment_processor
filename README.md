# Payment Processor

A mini payment processor built in Go to explore the hard parts of moving money: idempotency, transactional outbox, double-entry ledger, settlement and reconciliation.

> Work in progress — this is a learning/portfolio project. The card processor is mocked and no real cardholder data ever touches the system.

## Planned scope

- Payment lifecycle: authorization → capture → settlement, with refunds, voids and chargebacks
- Idempotent REST API (`Idempotency-Key` on every write)
- Transactional outbox publishing events to RabbitMQ, with idempotent consumers and DLQ
- Double-entry, append-only ledger
- Settlement file reconciliation
- Observability with Prometheus and Grafana

## Stack

Go · PostgreSQL · RabbitMQ · Docker Compose

