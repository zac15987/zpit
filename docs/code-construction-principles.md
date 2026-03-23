# Code Construction Principles

Core principles derived from *Code Complete* for use as an AI coding agent quality baseline.

---

## 1. Upstream Prerequisites: Measure Twice, Cut Once

- **Define the problem first.** Before writing any code, confirm the problem is clearly defined and requirements are understood.
- **Make architectural decisions upfront.** Data structures, key algorithms, and module boundaries should be settled before coding begins.
- **Defect cost escalates downstream.** Time invested upstream saves an order of magnitude downstream.

## 2. Design

- **Managing complexity is the central goal.** Every design technique exists to reduce the amount of complexity you must hold in your head at one time.
- **High cohesion, low coupling.** Each module does one thing well; dependencies between modules are minimized.
- **Information hiding.** A module's internal implementation is never exposed. The interface is the contract; the implementation is the secret.
- **Consistent abstraction levels.** Operations within a single routine should sit at the same level of abstraction.
- **Favor composition over inheritance.** Inheritance increases coupling; composition is more flexible.
- **No speculative generality.** Design for known requirements only. Do not add complexity for hypothetical future needs.

## 3. Classes and Routines

### Classes
- Each class represents one clear abstraction with a minimal public interface.
- Avoid God Classes. If a class has too many responsibilities, split it.
- Keep data members at roughly 7 +/- 2.

### Routines
- **Single responsibility.** A routine does one thing and does it well.
- **The name is the documentation.** A routine's name should fully describe its behavior without needing a comment to explain *what* it does.
- **Parameter count.** Keep under 7; ideally 3 or fewer.
- **Length.** Treat 200 lines as an upper-bound warning. Consider splitting beyond that.
- **No hidden side effects.** A routine must not do anything its name does not imply.

## 4. Defensive Programming

- **Validate at system boundaries.** All external input (user input, API responses, file reads) is untrusted.
- **Assertions for impossible conditions.** Use assertions for internal logic errors; use error handling for external errors.
- **Exceptions for exceptional conditions.** Never use exceptions to control normal flow.
- **Graceful degradation.** When errors occur, preserve as much functionality as possible and provide meaningful feedback.

## 5. Variables

- **Declare at first use.** Minimize the scope of every variable.
- **Single purpose.** Each variable carries exactly one meaning; never reuse a variable for a different purpose.
- **Naming conventions:**
  - Booleans: `is-`, `has-`, `can-` prefixes.
  - Loop indices: `i`, `j` for short scopes; meaningful names for long scopes.
  - Named constants replace magic numbers. Every literal should have a named constant.
- **Initialize at declaration.** Never use an uninitialized variable.

## 6. Control Structures

- **Limit nesting depth.** More than 3 levels of nesting calls for refactoring — extract routines, return early, or use guard clauses.
- **Loop discipline:**
  - Single entry, single exit.
  - Do not modify the loop control variable inside the loop body.
  - Prefer `for-each` over manual indexing.
- **Table-driven methods.** When logic consists of multi-layer `if-else` or `switch` chains, consider replacing with a lookup table.
- **Avoid goto.** In virtually all cases, goto is unnecessary.

## 7. Quality Practices

### Collaborative Construction
- **Code review before merge.** Reviews catch defects earlier and cheaper than testing.
- **Pair programming.** Reserve for complex or critical-path code.

### Testing
- **Test alongside development**, not as an afterthought.
- **Boundary conditions.** Test nulls, zeros, negatives, maximums, and off-by-one.
- **One regression test per bug.** After a fix, ensure the same defect cannot recur.
- **Coverage is necessary but not sufficient.** High coverage does not guarantee quality, but low coverage guarantees risk.

### Debugging
- **Reproduce first.** Stabilize the reproduction before attempting a fix.
- **Scientific method.** Hypothesize, predict, experiment, verify. Do not guess-and-patch.
- **Change one thing at a time.** Avoid multi-variable changes that prevent attribution.

## 8. Refactoring

- **Tests are a prerequisite.** Refactoring without test coverage is gambling.
- **Small steps.** One small change at a time; verify immediately after each change.
- **Common refactoring signals:**
  - Duplicated code (DRY violation)
  - Overly long routines or classes
  - Deep nesting
  - Long parameter lists
  - Shotgun surgery (one change requires edits in many places)
  - Feature envy (a routine heavily accesses another class's data)

## 9. Performance Tuning

- **Correct first, fast second.** Premature optimization is the root of all evil.
- **Measure before optimizing.** Use a profiler to locate bottlenecks. Never optimize by intuition.
- **The 80/20 rule.** 80% of execution time is spent in 20% of the code. Optimize only that 20%.
- **Algorithm-level gains first.** An O(n) to O(log n) improvement dwarfs any micro-optimization.
- **Measure again after optimizing.** Confirm the change actually improved performance and did not introduce regressions.

## 10. Layout and Self-Documenting Code

- **Consistent formatting.** Follow the project's existing style. Do not introduce personal preferences.
- **Code is the documentation.** Good naming and structure should make most comments unnecessary.
- **Comments explain *why*, not *what*.** The code already says *what*.
- **Delete commented-out code.** Version control remembers history.

## 11. Scale and Integration

- **Scale affects methodology.** Best practices for a 10-person team may not apply to a solo developer, and vice versa.
- **Incremental integration.** Continuous integration beats big-bang integration. Integrate a small piece, verify immediately.
- **Tools are force multipliers.** Leverage linters, formatters, CI/CD, and automated testing.

---

## Core Belief

> Managing complexity is the most critical technical topic in software construction.
> Every principle, pattern, and practice ultimately serves one goal:
> **Enable the developer (or AI agent) to work correctly while holding the minimum amount of code in mind at any given moment.**
