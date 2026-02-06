# Snipe CLI Testing Prompt

Copy this prompt into a Claude session with a Go codebase that has a snipe index built (`snipe index`). Pick a codebase you know well enough to judge the quality of Claude's answers.

---

## Prompt

You have access to `snipe`, a Go code navigation CLI. For this session, use snipe as your **primary** tool for understanding and navigating this codebase. Do NOT fall back to grep, ripgrep, or reading files unless snipe genuinely can't answer the question. The goal is to stress-test snipe and surface where it helps vs. where it falls short.

**Before you start**: Run `snipe status` and `snipe context --boot` to orient yourself.

### Phase 1: Orientation (5 min)

Use snipe to answer these questions about the codebase:

1. What are the 3-5 most architecturally significant types in this project? Use `snipe context --boot` and your own judgment. Did the boot context surface the right symbols, or did you have to dig?
2. Pick the most important package. Run `snipe pkg <name>`. Does the output give you a useful mental model of the package, or is it noise?
3. Find the main entry point and trace the startup flow using `snipe callers` and `snipe callees`. How many hops did it take to understand the initialization sequence?

### Phase 2: Real Questions (10 min)

Answer these as if a developer asked you. Use snipe commands, and note when you had to supplement with other tools.

4. "Who implements the most important interface in this codebase?" Use `snipe impl`. Did it find the right types?
5. "What does [pick a non-trivial function with 50+ lines] actually do?" Use `snipe explain`, then `snipe pack`. Which gave you more useful information? What was missing?
6. "I want to understand the data flow from [entry point] to [storage/output]." Trace it using `snipe callees` iteratively. How far did you get before you needed to read source?
7. "What would break if I changed the signature of [pick a widely-referenced type or function]?" Use `snipe refs` and `snipe callers` to assess blast radius.

### Phase 3: Modification Task (10 min)

8. Pick a function and plan a realistic refactor (extract a helper, change a parameter, rename something). Use snipe to:
   - Understand the function (`snipe def`, `snipe pack`)
   - Find all callers that would need updating (`snipe callers`)
   - Find all references (`snipe refs`)
   - Try a preview edit (`snipe edit <symbol> --operation replace_body --new-code "..."`)
   - Did the edit workflow feel natural or awkward? Did preview mode show you what would change?

### Phase 4: Advanced Features (5 min)

9. Use `snipe show <hex-id>` to recall a symbol you looked at earlier. Was the ID easy to grab from previous output?
10. Use `snipe search` for a string pattern. Compare the experience to `rg`. What did snipe add (or lose)?
11. Try `snipe types <type-name>` on a struct. Was the output useful?
12. Try `snipe importers <package>` to understand package dependencies.

### Phase 5: Friction Report

After completing the tasks above, give me a structured report:

**Commands ranked by usefulness** (1-10 scale, with justification):
- For each snipe command you used, rate how much it helped vs. just reading source files.

**Moments of delight**: Times snipe saved you real effort or gave you an insight you wouldn't have gotten from grep.

**Moments of frustration**: Times snipe's output was confusing, wrong, empty, or less useful than just reading the file. Be specific about what you expected vs. what you got.

**Missing capabilities**: Things you wanted to do but couldn't. Queries you wished existed.

**Output format feedback**: Was the JSON/human output helpful? What would you change about how results are presented?

**If snipe could do ONE thing better, what would have the highest impact on your workflow?**

---

## Notes for the tester

- Good test codebases: any Go project with 10k+ LOC, multiple packages, interfaces with implementations, and non-trivial call graphs. Examples: prometheus, caddy, hugo, consul, terraform, chi, echo, fzf/src.
- Build the index first: `snipe index` (add `--enrich` if you want to test explain/context quality with LLM enrichment).
- The prompt deliberately asks Claude to compare snipe to alternatives (reading source, grep). This surfaces whether snipe is actually faster/better, not just different.
- Phase 3 (modification) is the most important. If snipe can't support a realistic edit workflow end-to-end, it's a nav tool, not a dev tool.
- Run this against multiple codebases to distinguish "snipe bug" from "this codebase is weird."
