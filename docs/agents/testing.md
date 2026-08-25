# Testing

Use this policy when adding, reviewing, consolidating, or removing tests.

## Regression story

A test earns its place when it catches a plausible regression or independently
protects a meaningful seam, contract, or safety property. Be able to state the
test's **regression story**: what could break, why the failure matters to a
caller or operator, and why this test is an appropriate place to detect it.

Strong regression stories protect:

- observable module or product behavior;
- domain decisions with distinct caller consequences;
- boundaries with the filesystem, network, operating system, process model, or
  external representations;
- concurrency, cancellation, sequencing, retry, and recovery behavior;
- negative safety properties, such as preserving state the product does not
  own;
- architecture rules that ordinary behavior tests cannot enforce.

Overlap is justified when tests protect different seams or failure modes. A
unit test and an integration test are not duplicates merely because they pass
through some of the same code.

## Adding tests

Before adding a test:

1. Identify the behavior and its caller consequence.
2. Find existing tests for the same regression story.
3. Partition inputs by distinct behavior or consequence. Use one representative
   for inputs that are equivalent after validation or normalization.
4. Choose the narrowest public seam that proves the behavior. Use a broader
   seam when the interaction between components is itself the risk.
5. Assert the semantic result or externally visible effect, including the
   concrete error classification when callers depend on it.

The test is justified when its regression story is clear and existing coverage
does not protect that story at an equal or stronger seam.

## Low-signal tests

Remove, consolidate, or redesign a test when its main effect is one of these:

- repeating an implementation decision, such as a branch, literal, enum map,
  call sequence, or constructor guard, without protecting a stated contract;
- proving language behavior, exported field access, or ordinary slice, map, or
  pointer aliasing;
- constructing a state that the production boundary cannot emit;
- asserting a private intermediate representation already covered through the
  module's public behavior;
- repeating a stronger test at the same seam without adding a failure mode;
- enumerating inputs that converge on the same behavior and caller consequence;
- pinning obsolete identifiers or the absence of removed behavior when no
  compatibility or safety requirement remains;
- checking trivial delegation that performs no policy, transformation, error
  aggregation, or ownership-sensitive work;
- asserting only that an error exists when its classification or consequence is
  the behavior that matters;
- adding a weak happy-path check already subsumed by a stronger transition or
  failure-path test.

A direct mapping test can still be valuable when the mapping is domain policy.
In that case, test the policy exhaustively enough to prevent an unclassified
case rather than sampling a few implementation branches.

## Pruning tests

Classify each candidate as **keep**, **consolidate**, **redesign**, or **remove**.
Preserve the strongest test for each regression story and retain intentional
overlap across distinct seams. When behavior has been removed, remove its tests
unless a current compatibility or safety rule still requires the old behavior
to be recognized.

Before completing a pruning change, verify that every removed assertion is
either low-signal by this policy or covered by a named stronger test, then run
the affected test suites.
