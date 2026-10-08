// ORACLE STUB, not upstream. The real FeatureMap imports javafx.util.Pair; the
// probe needs only "record (namespace, name) under an idx", the research table.
package mage.player.ai.encoder;

import java.io.Serializable;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.TreeSet;

public class FeatureMap implements Serializable {
    public final Map<Integer, Set<String>> map = new TreeMap<>();

    public void addFeature(String name, long namespace, int idx) {
        map.computeIfAbsent(idx, k -> new TreeSet<>()).add(namespace + "/" + name);
    }
}
