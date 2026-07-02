# Tasks — brief --all fan-out (#207)

- [x] 1. TEST FIRST: `parseBriefArgs` accepts `--all` without `<desk>`; rejects `--all` + `<desk>`; interleaved desk position.
- [x] 2. Implement `flotilla brief --all` fan-out to non-primary-XO agents with dark-desk pre-check, continue-on-error, non-zero on any failure.
- [x] 3. Document coordinator doctrine: use `flotilla brief`, not free-text publish asks.
- [ ] 4. Gate: CI+cubic, trio PR comments, surface COS.