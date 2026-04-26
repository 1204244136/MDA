---
name: generate-pi-task
description: Generate or update MaaFramework Project Interface task JSON files based on pipeline definitions and interface specs. Use when creating, editing, or extending task files under assets/tasks/ that map pipeline nodes to user-facing options (switch/select/checkbox) with pipeline_override bindings.
compatibility: opencode
metadata:
    version: "1.0"
    project: MDA
---

## What I do

- Read pipeline JSON files and identify switchable nodes (those with `enabled` fields)
- Analyze node relationships to determine the correct option type (switch/select/checkbox/input)
- Generate or update `assets/tasks/*.json` files conforming to `interface_import.schema.json`
- Update `assets/interface.json` import array and locale files with new entries

## When to use me

Use this skill when:

- You need to create a new task file for a pipeline
- You need to add options to an existing task file
- A user mentions "generate task", "PI task", "pipeline option", or "interface task"
- A user is working on pipeline files and wants corresponding task entries

## Workflow

### Step 1: Read Specifications

Read these files from `deps/tools/`:

1. **`interface_import.schema.json`** — THE schema for task files (top-level: `task[]`, `option{}`, `preset[]`)
2. **`interface.schema.json`** — Main PI schema (for understanding import/group/controller/resource context)
3. **`pipeline.schema.json`** — Pipeline node schema (for understanding `enabled`, `next`, recognition fields)

### Step 2: Read Interface Context

1. Read **`assets/interface.json`**
2. Read ALL files in `import[]` array — collect existing task names and option keys to avoid conflicts
3. Note `group[]` definitions (e.g., "daily", "standalone")

### Step 3: Determine Target File

1. **Active file is in `import[]`?** → Edit that file
2. **Active file is a pipeline JSON?** → Derive pipeline name from directory (e.g., `pipeline/Battle/Battle.json` → "Battle"), then check if `assets/tasks/{Name}.json` exists → edit or create
3. **No active file context?** → Ask user which pipeline to generate for
4. **Creating new file?** → Also add entry to `interface.json`'s `import[]` array

### Step 4: Read Pipeline Files

1. Read ALL `.json` files in `assets/resource/pipeline/{PipelineName}/`
2. For each node, extract: name, `enabled` value, `next[]` targets, `desc`
3. Build a node graph: parent→children mapping via `next[]`

### Step 5: Analyze Nodes → Option Types

This is the core decision logic. See [Option Type Decision Reference](references/option-types.md) for details.

Quick rules:

- Single node with `enabled` → **switch** (Yes/No toggle)
- Sibling `enabled` nodes from same parent that are exclusive → **select** (each case enables self, disables siblings)
- Independent items where multiple can be active → **checkbox**
- Sub-choices revealed after selecting a case → **nested option** via `case.option[]`
- Configurable parameter → **input** with `pipeline_type`

### Step 6: Generate Task JSON

Follow `interface_import.schema.json` exactly. Template:

```json
{
    "task": [
        {
            "name": "PipelineName",
            "label": "$task.PipelineName.label",
            "entry": "PipelineName",
            "description": "$task.PipelineName.description",
            "option": ["OptionKey1"],
            "group": ["daily"]
        }
    ],
    "option": {
        "OptionKey1": {
            "type": "switch",
            "label": "$option.OptionKey1.label",
            "cases": [
                {"name": "Yes", "pipeline_override": {"TargetNode": {"enabled": true}}},
                {"name": "No", "pipeline_override": {"TargetNode": {"enabled": false}}}
            ]
        }
    }
}
```

### Step 7: Update Locale Files

Add i18n keys to `assets/locales/interface/zh_cn.json` and `en_us.json`:

- `task.{Name}.label` / `task.{Name}.description`
- `option.{OptionKey}.label`
- `option.{OptionKey}.{CaseName}` (for checkbox/select cases)
- Use `desc` from pipeline nodes as source for Chinese descriptions

### Step 8: Validate

- [ ] JSON syntax valid
- [ ] All `task[].option[]` keys exist in `option` object
- [ ] All `pipeline_override` target nodes exist in pipeline files
- [ ] All nested `case.option[]` references resolve to defined options
- [ ] New file added to `interface.json` `import[]` (if created)
- [ ] Locale keys added (both zh_cn and en_us)

## Gotchas

- **`interface_import.schema.json`** is the schema for task files, NOT `interface.schema.json` — the import schema only has `task`, `option`, `preset` at top level
- **`entry`** must exactly match the root pipeline node name (case-sensitive), NOT a sub-node
- **Import paths** in `interface.json` are relative to `interface.json` location: `"tasks/Arena.json"`, not `"assets/tasks/Arena.json"`
- **i18n `$` prefix** is required for all user-facing strings — never hardcode Chinese/English text directly
- **`enabled: false` in pipeline** means the option lets users turn it ON; `enabled: true` means turn it OFF
- **switch cases**: Must be exactly 2, names should be "Yes" and "No"
- **select cases**: Each case should set `enabled: true` for its own node AND `enabled: false` for sibling alternatives
- **checkbox cases**: Each case only sets `enabled: true` for its own node (no sibling disabling)
- **Some existing task files have `No` before `Yes`** in switch cases — this is intentional when default is off. Follow the pipeline's `enabled` default value ordering.
