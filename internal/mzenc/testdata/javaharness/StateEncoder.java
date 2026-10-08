// ORACLE STUB, not upstream. The real mage.player.ai.encoder.StateEncoder imports
// all of XMage; the probe needs only the two fields Features.addIndex writes to.
// Faithful for those two: featureVector receives exactly the ids addIndex emits,
// and featureMap records (namespace, name) per idx with the real class's key.
package mage.player.ai.encoder;

import java.util.HashSet;
import java.util.Set;

public class StateEncoder {
    public final Set<Integer> featureVector = new HashSet<>();
    public final FeatureMap featureMap = new FeatureMap();
}
