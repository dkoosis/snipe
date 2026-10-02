# Who calls snipe

Census of every program, hook, skill, rule and doc outside snipe that runs a snipe verb, so a verb rename is checked against callers instead of memory. Built 2026-09-30 for sn-lvub.1 (Every snipe verb is one an agent picks right the first time). Read-only record: it changes no caller and decides no verb (that is sn-zvoz).

Scope: every git repo under `~/projects/dk/` plus skills and rules under `~/nugbases/dk/`. Excluded: snipe's own tree (except `snipe/.claude/rules/CLAUDE.md`, a seed site), `.eval-repos/`, `node_modules/`, git worktrees and session logs. Paths are relative to `~/projects/dk/`. orca is retired (dk, 2026-09-30) and has no row.

Method: `rg -n -e '"snipe"' -e 'snipe <verb>\b'` over `*.{go,md,sh,yaml,yml,json}`, then one row per (file:line, verb). Flags and format are read from the same line; `concise (default)` means the line names no `--format`. Go test files that only fake snipe (sdlc `risk_test.go`, ferret and itzy fixtures) and ferret's transcript classifiers (`internal/event`, `internal/reach`, `internal/analyst`) name snipe as data, not as an invocation, so they have no row. Nugbase skills and rules hold no verb call.

Replacement column: blank in every row. The sn-zvoz decision fills it.

Kinds: hook, shell script, Go exec, agent prompt (agents and linters), skill, rule, doc. A `doc` or `rule` row is prose that tells a reader to run the verb; a hook or script row runs it.

543 rows.

| repo | file:line | verb | flags | format consumed | kind | replacement |
|---|---|---|---|---|---|---|
| cc-plugins | docs/exports/architecture-review-standalone.md:13 | index |  | concise (default) | doc | |
| cc-plugins | docs/exports/architecture-review-standalone.md:234 | impl |  | concise (default) | doc | |
| cc-plugins | plugins/boot/README.md:3 | context |  | concise (default) | doc | |
| cc-plugins | plugins/boot/scripts/grep-nudge.sh:19 | index |  | concise (default) | hook | |
| cc-plugins | plugins/boot/scripts/grep-nudge.sh:48 | def |  | concise (default) | hook | |
| cc-plugins | plugins/boot/scripts/session-start.sh:7 | context |  | concise (default) | hook | |
| cc-plugins | plugins/boot/scripts/session-start.sh:15 | context |  | concise (default) | hook | |
| cc-plugins | plugins/boot/scripts/session-start.sh:41 | context |  | concise (default) | hook | |
| cc-plugins | plugins/boot/scripts/session-start.sh:42 | context | timeout 8, SNIPE_NO_HEAL=1 | concise (default) | hook | |
| cc-plugins | plugins/boot/scripts/session-start.sh:48 | context |  | concise (default) | hook | |
| cc-plugins | plugins/boot/scripts/session-start.sh:50 | context |  | concise (default) | hook | |
| cc-plugins | plugins/boot/scripts/session-start.sh:63 | context |  | concise (default) | hook | |
| cc-plugins | plugins/boot/scripts/session-start.sh:71 | context |  | concise (default) | hook | |
| cc-plugins | plugins/boot/scripts/session-start.sh:92 | index | --embed-mode=off --enrich=false | concise (default) | hook | |
| cc-plugins | plugins/dispatch/scripts/lib.sh:220 | risk | --format concise | concise | shell script | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:39 | deps | --tree | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:40 | context |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:41 | hotspots | --top | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:44 | hotspots |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:46 | context | --full --max-tokens | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:46 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:48 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:58 | context | --full | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:59 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:59 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:60 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:60 | importers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:60 | types |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:61 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:61 | importers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:62 | refs |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:62 | search |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:63 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:63 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:63 | refs |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:64 | lits |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:64 | trace |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:66 | context | --full | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:66 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:66 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:79 | impact |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:79 | tests |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:93 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:96 | tests |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass1-correctness.md:106 | tests |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass2-concurrency.md:47 | pack | --signature-only | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass2-concurrency.md:48 | impact |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass2-concurrency.md:51 | metrics | --graph=calls --kind=cycles --format=json | json | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass2-concurrency.md:54 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass2-concurrency.md:56 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass2-concurrency.md:56 | impact |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass2-concurrency.md:60 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass2-concurrency.md:70 | callees |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass2-concurrency.md:98 | impact |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass2-concurrency.md:98 | tests |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass2-concurrency.md:113 | tests |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass2-concurrency.md:123 | tests |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass3-persistence.md:45 | importers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass3-persistence.md:46 | importers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass3-persistence.md:47 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass3-persistence.md:48 | types | --format detailed | detailed | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass3-persistence.md:49 | callees |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass3-persistence.md:50 | deps | --tree | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass3-persistence.md:56 | lifecycle | --depth | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass3-persistence.md:57 | lifecycle | --depth --include-tests | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass3-persistence.md:63 | lits |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass3-persistence.md:64 | trace |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass3-persistence.md:67 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass3-persistence.md:90 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass3-persistence.md:115 | impact |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass3-persistence.md:115 | tests |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass3-persistence.md:130 | tests |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/agents/pass3-persistence.md:140 | tests |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-bug-audit/references/finding-filing.md:37 | tests |  | concise (default) | doc | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/SKILL.md:43 | deps | --tree | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/SKILL.md:80 | tests |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/SKILL.md:89 | pack |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/SKILL.md:92 | tests |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/SKILL.md:102 | deps | --tree | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/SKILL.md:102 | pack |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/SKILL.md:116 | impact |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/SKILL.md:116 | tests |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/SKILL.md:129 | callers |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/SKILL.md:133 | callees |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/SKILL.md:149 | callees |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/SKILL.md:149 | deps |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/SKILL.md:155 | callers |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/SKILL.md:169 | impact |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/SKILL.md:170 | tests |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/SKILL.md:211 | tests |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/references/pipeline.md:72 | index |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/references/pipeline.md:73 | doctor |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/references/pipeline.md:75 | index |  | concise (default) | skill | |
| cc-plugins | plugins/go-bug-audit/skills/bug-audit/references/pipeline.md:296 | index |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/agents/go-test-architect.md:49 | context | --file | concise (default) | agent prompt | |
| cc-plugins | plugins/go-test-architect/agents/go-test-architect.md:50 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-test-architect/agents/go-test-architect.md:51 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-test-architect/agents/go-test-architect.md:67 | metrics | --kind=pagerank | concise (default) | agent prompt | |
| cc-plugins | plugins/go-test-architect/agents/go-test-architect.md:68 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-test-architect/agents/go-test-architect.md:69 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-test-architect/agents/go-test-architect.md:70 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-test-architect/agents/go-test-architect.md:71 | importers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/SKILL.md:196 | callers |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/SKILL.md:196 | importers |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/SKILL.md:196 | metrics | --kind=pagerank | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/SKILL.md:196 | pack |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/SKILL.md:242 | tests |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:8 | hotspots | --top | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:9 | risk |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:12 | pack |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:18 | pack |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:19 | pack |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:20 | callers |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:20 | impl |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:21 | importers |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:22 | impl |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:22 | importers |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:23 | importers |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:24 | importers |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:26 | hotspots |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:26 | metrics | --kind=pagerank | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:26 | pack |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:33 | tests |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:34 | callers |  | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:35 | metrics | --kind=pagerank | concise (default) | skill | |
| cc-plugins | plugins/go-test-architect/skills/go-testing/references/risk-scan.md:38 | tests |  | concise (default) | skill | |
| cc-plugins | plugins/kong-cli/agents/kong-cli.md:68 | context | --full | concise (default) | agent prompt | |
| cc-plugins | plugins/kong-cli/agents/kong-cli.md:69 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/kong-cli/agents/kong-cli.md:70 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/legibility/skills/legibility/SKILL.md:29 | pkg |  | concise (default) | skill | |
| cc-plugins | plugins/legibility/skills/legibility/SKILL.md:29 | sym |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/agents/dedup-analyze.md:27 | context |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/dedup-analyze.md:28 | pkg |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/dedup-analyze.md:29 | callers | --signature-only | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/dedup-analyze.md:30 | impact |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/dedup-analyze.md:37 | sim |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/dedup-analyze.md:39 | sim |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/dedup-analyze.md:41 | pkg | --format concise | concise | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/dedup-analyze.md:42 | embed-status |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/dedup-analyze.md:61 | sim |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/dedup-analyze.md:63 | callers | --signature-only | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/dedup-analyze.md:63 | explain |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/file-split.md:27 | context |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/file-split.md:28 | pkg | --format detailed --file | detailed | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/file-split.md:29 | callers | --signature-only | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/file-split.md:30 | sim |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/file-split.md:31 | impact |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/file-split.md:44 | pkg |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/file-split.md:48 | pkg | --file --format detailed | detailed | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/file-split.md:51 | sim |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/refactor.md:27 | context |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/refactor.md:28 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/refactor.md:29 | explain |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/refactor.md:30 | impact |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/refactor.md:31 | callers | --signature-only | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/refactor.md:32 | refs |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/refactor.md:33 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/refactor.md:34 | tests |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/refactor.md:37 | impact |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/refactor.md:37 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/refactor.md:118 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/refactor.md:119 | explain |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/refactor.md:120 | impact |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/refactor.md:121 | tests |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/agents/refactor.md:123 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:115 | doctor |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:115 | index | --embed-mode=off --enrich=false | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:116 | status |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:123 | context | --full | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:124 | deps | --tree | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:127 | context |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:128 | pkg | --format detailed | detailed | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:131 | context |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:132 | pkg | --format detailed | detailed | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:152 | context |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:153 | deps | --tree | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:156 | context |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:157 | deps | --tree | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:162 | context |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:163 | context | --full | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:168 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:169 | deps | --tree | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:170 | metrics | --kind=<kind> | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:176 | boundary |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:176 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/HARNESS.md:176 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/SWEEP.md:62 | pkg | --format detailed | detailed | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/TEMPLATE-linter.md:60 | context |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/TEMPLATE-linter.md:101 | context |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/TEMPLATE-linter.md:101 | doctor |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/internal/lint-harness/TEMPLATE-linter.md:101 | pkg |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/alloc-bounds.md:34 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/alloc-bounds.md:52 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/api-surface.md:25 | context | --full | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/api-surface.md:26 | types | --kind=interface --format=json | json | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/api-surface.md:39 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/api-surface.md:55 | impact |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/api-surface.md:55 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/api-surface.md:66 | sym | --format=json | json | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/api-surface.md:73 | importers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/api-surface.rules.md:28 | impact |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/arch.md:18 | boundary |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/arch.md:18 | diagram |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/arch.md:18 | lifecycle |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/arch.md:18 | metrics |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/arch.md:48 | metrics | --graph=imports --kind=cycles --format=json | json | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/arch.md:56 | metrics | --kind=ca --format=json | json | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/arch.md:57 | metrics | --kind=ce --format=json | json | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/arch.md:58 | metrics | --kind=instability --format=json | json | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/arch.md:59 | metrics | --kind=pagerank --format=json | json | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/arch.md:76 | boundary | --direction=a-to-b --detailed | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/arch.md:97 | pkg | --format detailed | detailed | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/arch.md:101 | context | --full | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/arch.md:104 | lifecycle | --depth --kg-hints | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/arch.md:109 | trace |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/arch.rules.md:101 | lifecycle |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/cohesion.md:13 | index |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/cohesion.md:13 | metrics |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/cohesion.md:20 | metrics |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/cohesion.md:21 | metrics |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/cohesion.md:27 | metrics | --kind=lcom4 --help | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/cohesion.md:38 | metrics | --kind=lcom4 --pkg={PKG} --format=detailed | detailed | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/cohesion.md:47 | boundary | --pkg={PKG} | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/cohesion.md:52 | metrics | --kind=instability --pkg={PKG} --format=json | json | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/cohesion.md:53 | metrics | --kind=abstractness --pkg={PKG} --format=json | json | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/cohesion.md:54 | metrics | --kind=distance --pkg={PKG} --format=json | json | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/cohesion.md:111 | pkg | --format detailed | detailed | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/concurrency-safety.md:42 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/concurrency-safety.md:48 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/concurrency-safety.md:50 | refs |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/concurrency-safety.md:60 | refs |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/concurrency-safety.md:69 | refs |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/concurrency-safety.md:79 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/conversion-drift.md:54 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/ctx-value.md:43 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/domain-model.md:13 | pkg |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/domain-model.md:13 | sym |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/domain-model.md:30 | pkg | --format=detailed | detailed | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/domain-model.md:56 | pkg | --format=summary | summary | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/domain-vocab.md:35 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/error-semantics.md:12 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/error-semantics.md:12 | refs |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/error-semantics.md:18 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/error-semantics.md:18 | refs |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/error-semantics.md:83 | callers | --format=summary | summary | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/error-semantics.md:84 | refs | --format=summary | summary | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/error-semantics.rules.md:63 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/error-semantics.rules.md:63 | refs |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/error-semantics.rules.md:76 | refs |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/error-semantics.rules.md:89 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/errors-design.md:35 | refs |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/goroutine-lifecycle.md:31 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/io-parallel.md:33 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/io-parallel.md:63 | metrics |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/n-plus-one.md:30 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/n-plus-one.md:48 | callees |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/n-plus-one.rules.md:24 | callees |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/pointer-value.md:52 | sym |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/sim-pair.md:19 | sim |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/sim-pair.md:31 | embed-status |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/sim-pair.md:32 | index | --embed | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/sim-pair.md:39 | pkg | --format json | json | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/sim-pair.md:42 | def | --signature-only | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/sim-pair.md:42 | sim |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/sim-pair.md:45 | callees |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/sim-pair.md:46 | callees |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/sim-pair.md:56 | sim |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/sim-pair.rules.md:5 | sim |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/simplify-flow.md:35 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/simplify-flow.md:36 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/simplify-shape.md:23 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/simplify-shape.md:23 | impact |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/simplify-shape.md:23 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/simplify-shape.md:23 | pkg |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/simplify-shape.md:32 | pkg | --format detailed | detailed | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/simplify-shape.md:36 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/simplify-shape.md:37 | impact |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/simplify-shape.md:39 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/simplify-shape.rules.md:10 | impact |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/simplify-shape.rules.md:10 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/slice-map.md:35 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/solid.md:25 | context | --full | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/solid.md:26 | types | --kind=interface | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/solid.md:33 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/solid.md:35 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/solid.md:42 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/solid.md:42 | types | --kind=struct | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/solid.md:46 | context |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/solid.md:58 | sym | --kind=interface --format=json | json | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/test-effectiveness.rules.md:90 | importers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/test-tables.md:35 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/testability.md:13 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/testability.md:13 | pkg |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/testability.md:13 | tests |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/testability.md:30 | pkg | --format detailed | detailed | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/testability.md:60 | pkg | --format=summary | summary | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/testability.md:70 | callers | --format=summary | summary | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/truthful-names.md:34 | context | --full | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/truthful-names.md:35 | metrics | --kind=pagerank | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/truthful-names.md:44 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/truthful-names.md:65 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/tx-boundary.md:38 | pack |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/vestige-pair.md:19 | refs |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/vestige-pair.md:34 | def |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/vestige-pair.md:35 | refs |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/vestige-pair.md:36 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/vestige-pair.md:42 | refs |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/vestige-pair.md:47 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/vestige-pair.rules.md:7 | callers |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/vestige-pair.rules.md:7 | refs |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/linters/vestige-pair.rules.md:14 | impl |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/references/review-all.md:27 | context |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/references/review-all.md:53 | index |  | concise (default) | agent prompt | |
| cc-plugins | plugins/lintbrush/scripts/snipe-bundle.sh:15 | metrics |  | concise (default) | shell script | |
| cc-plugins | plugins/lintbrush/scripts/snipe-bundle.sh:27 | index |  | concise (default) | shell script | |
| cc-plugins | plugins/lintbrush/scripts/snipe-bundle.sh:28 | index | --embed-mode=off --enrich=false | concise (default) | shell script | |
| cc-plugins | plugins/lintbrush/scripts/snipe-bundle.sh:34 | context |  | concise (default) | shell script | |
| cc-plugins | plugins/lintbrush/scripts/snipe-bundle.sh:38 | context | --full | concise (default) | shell script | |
| cc-plugins | plugins/lintbrush/scripts/snipe-bundle.sh:39 | context |  | concise (default) | shell script | |
| cc-plugins | plugins/lintbrush/scripts/snipe-bundle.sh:40 | deps | --tree | concise (default) | shell script | |
| cc-plugins | plugins/lintbrush/scripts/snipe-bundle.sh:42 | metrics | --kind | concise (default) | shell script | |
| cc-plugins | plugins/lintbrush/scripts/snipe-bundle.sh:45 | metrics | --graph=calls --kind=cycles | concise (default) | shell script | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:12 | index |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:31 | doctor |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:31 | index |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:31 | status |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:32 | index | --embed-mode=off --enrich=false | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:38 | context |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:39 | deps |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:40 | pkg |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:41 | importers |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:42 | imports |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:43 | impl |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:44 | types |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:45 | callers |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:46 | callees |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:47 | impact |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:48 | explain |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:49 | sim |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:50 | metrics | --graph=imports\ | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:51 | metrics | --graph=imports\ | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:52 | boundary |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:53 | lifecycle |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:54 | trace |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:55 | sym |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:56 | diagram |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:84 | metrics | --kind | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:154 | impact |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:154 | impl |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:156 | boundary |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:167 | pkg |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:177 | impact |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:177 | impl |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:188 | boundary | --direction=a-to-b --detailed | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:189 | metrics | --graph=imports --kind=cycles --format=json | json | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:196 | metrics | --graph=calls --kind=pagerank --top=20 | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:199 | pack |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:207 | lifecycle | --depth --kg-hints | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/architecture-patterns/SKILL.md:212 | trace |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/dedup-patterns/SKILL.md:68 | sim |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/dedup-patterns/SKILL.md:69 | sim |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/dedup-patterns/SKILL.md:70 | embed-status |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/dedup-patterns/SKILL.md:76 | search |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/dedup-patterns/SKILL.md:82 | pack |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/dedup-patterns/SKILL.md:83 | pack |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/dedup-patterns/SKILL.md:89 | refs |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/dedup-patterns/SKILL.md:90 | callers | --signature-only | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/dedup-patterns/SKILL.md:99 | callers | --signature-only | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/dedup-patterns/SKILL.md:100 | callees | --signature-only | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/dedup-patterns/SKILL.md:101 | impact |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/dedup-patterns/SKILL.md:107 | pack |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/dedup-patterns/SKILL.md:108 | pack |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/file-splitting/SKILL.md:159 | context |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/file-splitting/SKILL.md:165 | pkg | --file --format detailed | detailed | skill | |
| cc-plugins | plugins/lintbrush/skills/file-splitting/SKILL.md:174 | callers | --signature-only | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/file-splitting/SKILL.md:175 | refs |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/file-splitting/SKILL.md:184 | callees | --signature-only | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/file-splitting/SKILL.md:192 | sim |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/file-splitting/SKILL.md:193 | impact |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/SKILL.md:36 | hotspots |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/SKILL.md:41 | doctor |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/SKILL.md:41 | index | --embed-mode=off --enrich=false | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/SKILL.md:42 | context | --full | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/SKILL.md:55 | metrics | --graph=imports --kind=pagerank | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/SKILL.md:56 | metrics | --graph=imports --kind=cycles | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/SKILL.md:57 | boundary | --detailed | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/SKILL.md:58 | impact |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/SKILL.md:58 | importers |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/SKILL.md:59 | pkg |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/SKILL.md:60 | tests |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/SKILL.md:79 | diagram |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:10 | impl |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:18 | def |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:18 | refs |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:24 | importers |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:24 | pkg |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:24 | sim |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:30 | boundary | --detailed | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:38 | callers |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:38 | explain |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:43 | tests |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:51 | callees |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:51 | tests |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:58 | importers |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:58 | pkg |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:65 | deps |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:70 | hotspots |  | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/go-smells.md:70 | metrics | --kind=pagerank | concise (default) | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/report.md:78 | diagram | --format=svg | svg | skill | |
| cc-plugins | plugins/lintbrush/skills/go-arch-review/references/report.md:79 | diagram | --format=svg | svg | skill | |
| cc-plugins | plugins/transform/skills/d2/SKILL.md:4 | diagram |  | concise (default) | skill | |
| cc-plugins | plugins/transform/skills/d2/SKILL.md:12 | diagram |  | concise (default) | skill | |
| cc-plugins | plugins/transform/skills/d2/SKILL.md:25 | diagram |  | concise (default) | skill | |
| cc-plugins | plugins/transform/skills/d2/SKILL.md:28 | diagram | --help | concise (default) | skill | |
| cc-plugins | plugins/transform/skills/d2/SKILL.md:28 | index |  | concise (default) | skill | |
| cc-plugins | plugins/transform/skills/d2/SKILL.md:77 | diagram |  | concise (default) | skill | |
| cc-plugins | plugins/transform/skills/d2/SKILL.md:81 | index |  | concise (default) | skill | |
| cc-plugins | plugins/transform/skills/d2/SKILL.md:82 | diagram | --format concise | concise | skill | |
| cc-plugins | plugins/transform/skills/d2/eval.yaml:1 | diagram |  | concise (default) | skill | |
| cc-plugins | plugins/transform/skills/d2/eval.yaml:10 | diagram |  | concise (default) | skill | |
| cc-plugins | plugins/transform/skills/d2/eval.yaml:27 | diagram |  | concise (default) | skill | |
| cc-plugins | plugins/transform/skills/d2/eval.yaml:28 | arch |  | concise (default) | skill | |
| cc-plugins | plugins/transform/skills/d2/eval.yaml:47 | diagram |  | concise (default) | skill | |
| cc-plugins | plugins/transform/skills/d2/eval.yaml:52 | diagram |  | concise (default) | skill | |
| conform-to-sdlc | internal/sandbox/assets/lib/lib-activate.sh:64 | def |  | concise (default) | shell script | |
| conform-to-sdlc | internal/sandbox/assets/lib/lib-activate.sh:65 | callers |  | concise (default) | shell script | |
| conform-to-sdlc | internal/sandbox/assets/lib/lib-activate.sh:66 | search |  | concise (default) | shell script | |
| conform-to-sdlc | internal/sandbox/assets/lib/lib-setup.sh:56 | index |  | concise (default) | shell script | |
| conform-to-sdlc | internal/sandbox/assets/lib/lib-setup.sh:63 | index |  | concise (default) | shell script | |
| conform-to-sdlc | internal/sandbox/assets/lib/lib-setup.sh:63 | index | --embed-mode=off --enrich=false | concise (default) | shell script | |
| conform-to-sdlc | internal/sandbox/assets/lib/lib-setup.sh:69 | index |  | concise (default) | shell script | |
| conform-to-sdlc | internal/sandbox/assets/lib/lib-setup.sh:73 | index | --embed-mode=off --enrich=false | concise (default) | shell script | |
| conform-to-sdlc | internal/sandbox/assets/lib/lib-setup.sh:73 | index |  | concise (default) | shell script | |
| fo | docs/archive/plans/2026-02-26-qa-report-pipeline.md:30 | index |  | concise (default) | doc | |
| fo | docs/feedback/review/arch-repo.md:8 | metrics | --graph=imports --kind=cycles | concise (default) | doc | |
| fo | docs/feedback/review/change-smells-repo.md:82 | deps | --tree | concise (default) | doc | |
| fo | docs/feedback/review/sim-pair-repo.md:10 | sim |  | concise (default) | doc | |
| fo | docs/feedback/review/sim-pair-repo.md:16 | embed-status |  | concise (default) | doc | |
| fo | docs/feedback/review/sim-pair-repo.md:17 | index | --embed | concise (default) | doc | |
| fo | docs/feedback/review/sim-pair-repo.md:27 | embed-status |  | concise (default) | doc | |
| fo | docs/feedback/review/sim-pair-repo.md:33 | sim |  | concise (default) | doc | |
| fo | docs/feedback/review/sim-pair-repo.md:37 | sim |  | concise (default) | doc | |
| fo | docs/feedback/review/sim-pair-repo.md:38 | index | --embed | concise (default) | doc | |
| fo | docs/feedback/review/sim-pair-repo.md:45 | sim |  | concise (default) | doc | |
| fo | docs/feedback/review/sim-pair-repo.md:48 | index | --embed | concise (default) | doc | |
| fo | docs/feedback/review/test-effectiveness-repo.md:23 | importers |  | concise (default) | doc | |
| fo | docs/feedback/review/tx-boundary-repo.md:30 | metrics |  | concise (default) | doc | |
| itzy | docs/diagrams/arch.md:1 | diagram |  | concise (default) | doc | |
| itzy | docs/diagrams/arch.md:122 | arch |  | concise (default) | doc | |
| itzy | docs/diagrams/datastore-map.md:1 | diagram |  | concise (default) | doc | |
| itzy | docs/diagrams/seam-map.md:1 | diagram |  | concise (default) | doc | |
| itzy | docs/diagrams/system-map.md:1 | diagram |  | concise (default) | doc | |
| itzy | docs/plans/reviews/iz-avl.17-bundle-static.md:153 | context |  | concise (default) | doc | |
| itzy | docs/plans/reviews/iz-avl.17-bundle-static.md:154 | pack |  | concise (default) | doc | |
| itzy | docs/plans/reviews/iz-avl.17-bundle-static.md:155 | def |  | concise (default) | doc | |
| itzy | docs/plans/reviews/iz-avl.17-bundle-static.md:156 | callers |  | concise (default) | doc | |
| itzy | docs/plans/reviews/iz-avl.17-bundle-static.md:157 | callees |  | concise (default) | doc | |
| itzy | docs/plans/reviews/iz-avl.17-bundle-static.md:158 | refs |  | concise (default) | doc | |
| itzy | docs/plans/reviews/iz-avl.17-bundle-static.md:159 | tests |  | concise (default) | doc | |
| itzy | docs/plans/reviews/iz-avl.17-bundle-static.md:160 | lifecycle |  | concise (default) | doc | |
| itzy | docs/plans/reviews/iz-avl.17-bundle-static.md:161 | impl |  | concise (default) | doc | |
| itzy | docs/plans/reviews/iz-avl.17-bundle-static.md:165 | callers |  | concise (default) | doc | |
| itzy | docs/plans/reviews/iz-r8r-bundle-static.md:108 | impact |  | concise (default) | doc | |
| itzy | docs/plans/reviews/iz-sz0.2-bundle-static.md:37 | pack |  | concise (default) | doc | |
| itzy | docs/plans/reviews/iz-sz0.2-pass-2-persistence.md:63 | tests |  | concise (default) | doc | |
| itzy | docs/plans/reviews/iz-sz0.6-bundle-static.md:35 | impact |  | concise (default) | doc | |
| mnemd | .sandbox/lib/lib-activate.sh:64 | def |  | concise (default) | shell script | |
| mnemd | .sandbox/lib/lib-activate.sh:65 | callers |  | concise (default) | shell script | |
| mnemd | .sandbox/lib/lib-activate.sh:66 | search |  | concise (default) | shell script | |
| mnemd | .sandbox/lib/lib-setup.sh:56 | index |  | concise (default) | shell script | |
| mnemd | .sandbox/lib/lib-setup.sh:63 | index |  | concise (default) | shell script | |
| mnemd | .sandbox/lib/lib-setup.sh:63 | index | --embed-mode=off --enrich=false | concise (default) | shell script | |
| mnemd | .sandbox/lib/lib-setup.sh:69 | index |  | concise (default) | shell script | |
| mnemd | .sandbox/lib/lib-setup.sh:73 | index |  | concise (default) | shell script | |
| mnemd | .sandbox/lib/lib-setup.sh:73 | index | --embed-mode=off --enrich=false | concise (default) | shell script | |
| sdlc | cmd/sdlc/review.go:144 | risk | --format=json | json | go | |
| sdlc | docs/adr/0002-work-provenance-lives-in-commit-trailers.md:27 | index |  | concise (default) | doc | |
| sdlc | home/rules/standard-go-tools.md:10 | callers |  | concise (default) | rule | |
| sdlc | home/rules/standard-go-tools.md:10 | context |  | concise (default) | rule | |
| sdlc | home/rules/standard-go-tools.md:10 | impact |  | concise (default) | rule | |
| sdlc | home/rules/standard-go-tools.md:10 | pack |  | concise (default) | rule | |
| sdlc | plugins/sdlc/cmd/sdlc/risk.go:559 | impact | <symbol> --format=json | json — passed through whole, no field read | Go exec | |
| sdlc | plugins/sdlc/cmd/sdlc/risk.go:779 | triage | --format=json <files...> | json — reads top-level files[] (path, score, fan_in, cyclo per row), files_with_tests[], package_count | Go exec | |
| sdlc | plugins/sdlc/exemplars/test/reference-exemplar-answered-findings.md:15 | index |  | concise (default) | doc | |
| sdlc | plugins/sdlc/references/reference-risk-schema.json:3 | impact |  | json (describes risk.go:559) | doc | |
| sdlc | plugins/sdlc/references/reference-risk-schema.json:3 | triage |  | json (describes risk.go:779) | doc | |
| sdlc | plugins/sdlc/references/reference-shared-checkout.md:19 | impact |  | concise (default) | doc | |
| sdlc | plugins/sdlc/skills/discover/shape/SKILL.md:36 | context |  | concise (default) | skill | |
| snipe | .claude/rules/CLAUDE.md:44 | def | <Symbol> | concise (default) | rule | |
| snipe | .claude/rules/CLAUDE.md:45 | def | --at file:L:C | concise (default) | rule | |
| snipe | .claude/rules/CLAUDE.md:46 | callees |  | concise (default) | rule | |
| snipe | .claude/rules/CLAUDE.md:46 | callers |  | concise (default) | rule | |
| snipe | .claude/rules/CLAUDE.md:46 | refs |  | concise (default) | rule | |
| snipe | .claude/rules/CLAUDE.md:47 | show | <hex-id> | concise (default) | rule | |
| snipe | .claude/rules/CLAUDE.md:49 | context |  | concise (default) | rule | |
| snipe | .claude/rules/CLAUDE.md:50 | context | --full | concise (default) | rule | |
| snipe | .claude/rules/CLAUDE.md:51 | context | --out DIR | concise (default) | rule | |
| trixi | AGENTS.md:214 | def |  | concise (default) | rule | |
| trixi | AGENTS.md:215 | callers |  | concise (default) | rule | |
| trixi | AGENTS.md:216 | refs |  | concise (default) | rule | |
| trixi | AGENTS.md:217 | search |  | concise (default) | rule | |
| trixi | AGENTS.md:218 | pack |  | concise (default) | rule | |
| trixi | AGENTS.md:219 | context | --boot | concise (default) | rule | |
| trixi | AGENTS.md:224 | def |  | concise (default) | rule | |
| trixi | AGENTS.md:224 | search |  | concise (default) | rule | |
| trixi | config/skills/issue-gate/SKILL.md:19 | context |  | concise (default) | skill | |
| trixi | config/skills/issue-gate/SKILL.md:20 | pack |  | concise (default) | skill | |
| trixi | config/skills/issue-gate/SKILL.md:21 | callers |  | concise (default) | skill | |
| trixi | config/skills/issue-gate/SKILL.md:22 | tests |  | concise (default) | skill | |
| trixi | config/skills/write-issue/SKILL.md:21 | context |  | concise (default) | skill | |
| trixi | config/skills/write-issue/SKILL.md:22 | pack |  | concise (default) | skill | |
| trixi | config/skills/write-issue/SKILL.md:23 | tests |  | concise (default) | skill | |
| trixi | config/skills/write-issue/SKILL.md:97 | orient |  | concise (default) | skill | |
| trixi | docs/feedback/review-all-2026-05-17.md:6 | context |  | concise (default) | doc | |
| trixi | docs/feedback/review-all-2026-05-17.md:6 | context | --full | concise (default) | doc | |
| trixi | docs/feedback/review-all-2026-05-17.md:75 | context | --full | concise (default) | doc | |
| trixi | docs/feedback/review-all-2026-05-17.md:75 | context |  | concise (default) | doc | |
| trixi | docs/feedback/review/arch-modules-persistence-store.md:28 | index |  | concise (default) | doc | |
| trixi | docs/feedback/review/arch-modules-persistence-store.md:39 | metrics | --kind=cycles | concise (default) | doc | |
| trixi | docs/feedback/review/arch-modules-persistence-store.md:43 | lifecycle |  | concise (default) | doc | |
| trixi | docs/feedback/review/arch-project.md:11 | metrics | --kind=cycles | concise (default) | doc | |
| trixi | docs/feedback/review/arch-repo.md:104 | boundary |  | concise (default) | doc | |
| trixi | docs/feedback/review/change-smells-repo.md:164 | imports |  | concise (default) | doc | |
| trixi | docs/feedback/review/sqlite-go-review-2026-07-15.md:74 | context |  | concise (default) | doc | |
| trixi | docs/feedback/review/sqlite-go-review-2026-07-15.md:74 | doctor |  | concise (default) | doc | |
| trixi | docs/feedback/review/sqlite-go-review-2026-07-15.md:74 | index |  | concise (default) | doc | |
| trixi | docs/feedback/review/sqlite-go-review-2026-07-15.md:75 | context |  | concise (default) | doc | |
| trixi | docs/feedback/review/sqlite-go-review-2026-07-15.md:75 | doctor |  | concise (default) | doc | |
| trixi | docs/feedback/review/sqlite-go-review-2026-07-15.md:75 | index |  | concise (default) | doc | |
| trixi | docs/feedback/review/sqlite-go-review-2026-07-15.md:76 | search |  | concise (default) | doc | |
| trixi | docs/feedback/review/truthful-names-project.md:53 | pack |  | concise (default) | doc | |
| trixi | docs/feedback/review/truthful-names-repo.md:156 | context | --pkg | concise (default) | doc | |
| trixi | docs/feedback/review/truthful-names-repo.md:156 | metrics | --kind=pagerank | concise (default) | doc | |
| trixi | docs/feedback/review/vestige-pair-nug.md:44 | refs |  | concise (default) | doc | |
| trixi | docs/feedback/review/vestige-pair-nug.md:90 | callers |  | concise (default) | doc | |
| upcheck | .sandbox/lib/lib-activate.sh:64 | def |  | concise (default) | shell script | |
| upcheck | .sandbox/lib/lib-activate.sh:65 | callers |  | concise (default) | shell script | |
| upcheck | .sandbox/lib/lib-activate.sh:66 | search |  | concise (default) | shell script | |
| upcheck | .sandbox/lib/lib-setup.sh:56 | index |  | concise (default) | shell script | |
| upcheck | .sandbox/lib/lib-setup.sh:63 | index | --embed-mode=off --enrich=false | concise (default) | shell script | |
| upcheck | .sandbox/lib/lib-setup.sh:63 | index |  | concise (default) | shell script | |
| upcheck | .sandbox/lib/lib-setup.sh:69 | index |  | concise (default) | shell script | |
| upcheck | .sandbox/lib/lib-setup.sh:73 | index |  | concise (default) | shell script | |
| upcheck | .sandbox/lib/lib-setup.sh:73 | index | --embed-mode=off --enrich=false | concise (default) | shell script | |

## Verbs with no external caller

Every verb in `snipe --help` (2026-09-30) that has no row above:

`verify`, `plan`, `guard`, `sensitive`, `c4`, `deadcode`, `report`, `edit`, `version`

Of the verbs the seed census expected to be uncalled, these gained a caller and have rows above: `orient`, `sym`, `explain`, `show`, `lits`, `trace`, `imports`, `types`. Check the rows: some are prose mentions in skills, not runs.
