# Option Type Decision Reference

Detailed guide for analyzing pipeline nodes and selecting the correct option type.

## Node Analysis Process

### 1. Identify Switchable Nodes

Scan all pipeline nodes for the `enabled` property:

| `enabled` value | Meaning                | Option behavior                              |
| --------------- | ---------------------- | -------------------------------------------- |
| `false`         | Node is OFF by default | Option lets user turn it ON                  |
| `true`          | Node is ON by default  | Option lets user turn it OFF                 |
| absent          | Not a switchable node  | Skip (unless it's a routing node for select) |

Nodes with `enabled` are your primary candidates for options.

### 2. Build Sibling Groups

From each parent node's `next[]` array, identify which children are **siblings** (same parent). Then check which siblings have `enabled` fields.

**Example: `FlagInInterception.next[]`**

```text
next: ["NormalyInterception", "AnomalyInterception", "EndTask"]
```

- `NormalyInterception` has `enabled: false` ✓
- `AnomalyInterception` has `enabled: false` ✓
- `EndTask` has no `enabled` — not a switchable candidate

→ Sibling group: {NormalyInterception, AnomalyInterception}

### 3. Classify Each Group

For each sibling group, determine if they are **mutually exclusive** or **independent**:

#### Mutual Exclusion → `select`

**Signals:**

- Nodes represent alternative routes (only one should run)
- Semantic grouping: same domain with different variants (e.g., "Normal" vs "Anomaly", "LevelD"/"LevelS"/"LevelEX")
- The parent node branches to them as alternatives

**Pipeline override pattern:**

```json
{
    "type": "select",
    "cases": [
        {
            "name": "OptionA",
            "pipeline_override": {
                "NodeA": {"enabled": true},
                "NodeB": {"enabled": false}
            }
        },
        {
            "name": "OptionB",
            "pipeline_override": {
                "NodeA": {"enabled": false},
                "NodeB": {"enabled": true}
            }
        }
    ],
    "default_case": "OptionB"
}
```

#### Independent Items → `checkbox`

**Signals:**

- Nodes represent independent purchasable/selectable items
- Multiple can be active simultaneously without conflict
- Shopping lists, feature toggles, tower entries

**Pipeline override pattern:**

```json
{
    "type": "checkbox",
    "default_case": [],
    "cases": [
        {
            "name": "ItemA",
            "pipeline_override": {"NodeA": {"enabled": true}}
        },
        {
            "name": "ItemB",
            "pipeline_override": {"NodeB": {"enabled": true}}
        }
    ]
}
```

**Important:** Unlike select, checkbox cases do NOT disable siblings. Each case only enables its own node.

#### Single Toggle → `switch`

**Signals:**

- Node is standalone, not part of a sibling group
- Simple on/off behavior
- The most common option type

**Pipeline override pattern:**

```json
{
    "type": "switch",
    "cases": [
        {"name": "Yes", "pipeline_override": {"Node": {"enabled": true}}},
        {"name": "No", "pipeline_override": {"Node": {"enabled": false}}}
    ]
}
```

**Default case:** Add `"default_case": "Yes"` if the option should be on by default for most users.

### 4. Identify Nested Options

After determining a case's pipeline_override target, check if that target node's `next[]` contains further switchable sub-nodes.

**Detection:**

- Case enables `NodeA` via pipeline_override
- `NodeA.next[]` contains `SubNode1`, `SubNode2`, etc. with `enabled` fields
- These sub-nodes become a **nested option** accessible only when `NodeA` is active

**Pattern:**

```json
{
    "name": "Yes",
    "pipeline_override": {"NodeA": {"enabled": true}},
    "option": ["SubOptionName"]
}
```

Then define `SubOptionName` as a separate option entry (select, switch, or checkbox as appropriate for the sub-nodes).

### 5. Identify Input Options

Rare in current codebase but supported by schema. Use when a pipeline node accepts a configurable value:

```json
{
    "type": "input",
    "inputs": [
        {
            "name": "FieldName",
            "pipeline_type": "int",
            "default": "5"
        }
    ],
    "pipeline_override": {
        "NodeName": {"property": "「FieldName」"}
    }
}
```

The `「名称」` format references the input field's value at runtime.

## Real Examples from This Project

| Task File           | Option                    | Type            | Why                                                     |
| ------------------- | ------------------------- | --------------- | ------------------------------------------------------- |
| Arena.json          | SpecialReward             | switch          | Single toggle (claim reward or not)                     |
| Arena.json          | EnterRookieArena          | switch          | Single toggle (enter this arena or not)                 |
| Interception.json   | InterceptionType          | select          | Mutual exclusion: Normal OR Anomaly                     |
| Interception.json   | NormalyInterceptionLevel  | select (nested) | Sub-select: which difficulty level                      |
| Interception.json   | AnomalyInterceptionTarget | select (nested) | Sub-select: which anomaly boss                          |
| Interception.json   | ManualInterceptionBattle  | switch          | Single toggle                                           |
| Shop.json           | ArenaShopItemList         | checkbox        | Multiple independent shop items                         |
| Shop.json           | RecyclingShopList         | checkbox        | Multiple independent recyclable items, default: ["Gem"] |
| Shop.json           | CommonShopFreeGoods       | switch          | Single toggle                                           |
| SimulationRoom.json | StartOverlock             | switch          | Single toggle, Yes case nests AutoBIOSSetting           |
| SimulationRoom.json | AutoBIOSSetting           | switch (nested) | Sub-switch: automatic difficulty                        |
| Output.json         | DefenseRewards            | switch          | Default ON (default_case: "Yes")                        |
| TribeTower.json     | EnterCommonTower          | switch          | Default OFF, No before Yes in cases                     |
