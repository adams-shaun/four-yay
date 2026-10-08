# mzenc oracle harness

Generates the golden vectors in `../golden/*.json` that the Go port must replay
byte-for-byte. **Manual, one-off**: the normal `go test` loop reads the committed
JSON and needs no JVM.

## Files

| File | What |
|---|---|
| `Features.java` | vendored from `WillWroble/mage` @ master (v0.2, `cb7e9c6f`), `Mage.Server.Plugins/Mage.Player.AI/.../encoder/Features.java`. Only changes: the unused `import org.apache.log4j.Logger;` removed (so plain `javac` needs no classpath), and two `__vec`/`__fm` accessors added for the probe. |
| `StateEncoder.java` | **stub** (`Set<Integer> featureVector`, `FeatureMap featureMap`). The real class imports all of XMage; the probe needs only the two fields `Features.addIndex` writes to. |
| `FeatureMap.java` | **stub**. The real class imports `javafx.util.Pair`; the probe only records `idx -> (namespace, name)`. |
| `FeaturesProbe.java` | reads one op sequence on stdin, prints the Java ids. |

The stubs are faithful for what the oracle reads: `featureVector` receives
exactly the ids `Features.addIndex` emits, and `featureMap` keys `(namespace,
name)` per idx the same way the shipped class does.

## Op schema

stdin is one JSON object:

```json
{"useFeatureMap": false, "ops": [
  {"op": "feature",  "name": "Card"},
  {"op": "numeric",  "name": "Power", "num": 50},
  {"op": "push",     "name": "Battlefield", "passToParent": false},
  {"op": "feature",  "name": "Tapped"},
  {"op": "pop"}
]}
```

- `feature` → `Features.addFeature(name, callParent)` (default `callParent:true`).
- `numeric` → `addNumericFeature(name, num, callParent)`.
- `push` → `getSubFeatures(name, passToParent)` (default `passToParent:true`); descends.
- `pop` → ascend (the root is never popped).

stdout: `{"indices":[...]}` plus `"map":{idx:[ "namespace/name", ... ]}` when
`useFeatureMap` is true.

## Regenerate

```sh
MZENC_JDK=/mnt/sata/gorge-training/xmageoracle/jdk   # or any JDK 17
J=/home/sadams/projects/gorge/.worktrees/mzenc/internal/mzenc/testdata/javaharness
$MZENC_JDK/bin/javac -d /tmp/mzenc-probe $J/*.java
for f in .../testdata/golden/*.json; do
  jq -c '{useFeatureMap:(.useFeatureMap//false),ops:.ops}' "$f" \
    | $MZENC_JDK/bin/java -cp /tmp/mzenc-probe mage.player.ai.encoder.FeaturesProbe
done   # paste each printed "indices" (and "map") back into that golden
```

Never hand-edit `indices`; if a golden disagrees with the Go port, one of the two
is wrong — regenerate from the probe.
