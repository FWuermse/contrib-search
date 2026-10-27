# Platform Mesh Search Provider RFCs

This directory contains Request for Comments (RFCs) documents that describe architectural designs and technical proposals for Platform Mesh's Search Provider.

## Active RFCs

- [RFC-001: Search Architecture](./rfc-001-search-architecture.md) - Generic search engine architecture with OpenFGA integration (Status: Draft)

## RFC Process

RFCs follow the template structure defined in the [ApeiroRA Architecture Repository](https://github.tools.sap/ApeiroRA/architecture/tree/main/rfc).

### RFC Structure

Each RFC should include:

1. **Status**: Draft, Collecting Feedback, Accepted, Rejected, Abandoned
2. **Authors**: RFC authors and contributors
3. **Context and Problem Statement**: Why this change is needed
4. **Approach**: Detailed technical design
5. **Pros & Cons**: Trade-offs and considerations
6. **Alternatives**: Other approaches considered
7. **Appendix and References**: Supporting materials

### Creating a New RFC

1. Copy `rfc-000-template.md` from the ApeiroRA repository
2. Number your RFC sequentially (e.g., `rfc-002-title.md`)
3. Add relevant diagrams to `assets/` directory
4. Update this README with your RFC

## Relationship to ADRs

- **ADRs** (Architecture Decision Records): Document decisions made
- **RFCs**: Propose and design solutions before decisions are made

RFCs typically precede ADRs. Once an RFC is accepted and implemented, the decision is captured in an ADR.
