// ORACLE GENERATOR, not upstream. Reads one {"useFeatureMap":bool,"ops":[...]}
// object on stdin, drives the vendored Features tree, prints
// {"indices":[...]} (+ "map" when useFeatureMap). See README.md.
package mage.player.ai.encoder;

import java.io.*;
import java.nio.charset.StandardCharsets;
import java.util.*;

public final class FeaturesProbe {

    public static void main(String[] args) throws Exception {
        String in = new String(System.in.readAllBytes(), StandardCharsets.UTF_8);
        boolean useFm = in.contains("\"useFeatureMap\":true");
        Features.useFeatureMap = useFm;

        StateEncoder enc = new StateEncoder();
        Features root = new Features();
        root.setEncoder(enc);
        Deque<Features> st = new ArrayDeque<>();
        st.push(root);

        for (Map<String, Object> op : parseOps(in)) {
            String k = (String) op.get("op");
            String name = (String) op.getOrDefault("name", "");
            int num = op.get("num") instanceof Integer ? (Integer) op.get("num") : 0;
            boolean callParent = !Boolean.FALSE.equals(op.get("callParent"));
            boolean passToParent = !Boolean.FALSE.equals(op.get("passToParent"));
            switch (k) {
                case "feature": st.peek().addFeature(name, callParent); break;
                case "numeric": st.peek().addNumericFeature(name, num, callParent); break;
                case "push":    st.push(st.peek().getSubFeatures(name, passToParent)); break;
                case "pop":     if (st.size() > 1) st.pop(); break;
                default: throw new IllegalArgumentException("unknown op " + k);
            }
        }

        List<Integer> idx = new ArrayList<>(enc.featureVector);
        Collections.sort(idx);
        StringBuilder sb = new StringBuilder("{\"indices\":[");
        for (int i = 0; i < idx.size(); i++) {
            if (i > 0) sb.append(',');
            sb.append(idx.get(i));
        }
        sb.append(']');
        if (useFm) {
            sb.append(",\"map\":{");
            boolean first = true;
            for (Map.Entry<Integer, Set<String>> e : enc.featureMap.map.entrySet()) {
                if (!first) sb.append(',');
                first = false;
                sb.append('"').append(e.getKey()).append("\":[");
                boolean f2 = true;
                for (String v : e.getValue()) {
                    if (!f2) sb.append(',');
                    f2 = false;
                    sb.append('"').append(v).append('"');
                }
                sb.append(']');
            }
            sb.append('}');
        }
        sb.append('}');
        System.out.println(sb);
    }

    // parseOps: fixed-schema scanner for {"op":..,"name":..,"num":..,"passToParent":..,"callParent":..}.
    static List<Map<String, Object>> parseOps(String s) {
        List<Map<String, Object>> out = new ArrayList<>();
        int i = s.indexOf("\"ops\"");
        if (i < 0) return out;
        i = s.indexOf('[', i);
        if (i < 0) return out;
        int j = i + 1;
        while (true) {
            int a = s.indexOf('{', j);
            int b = s.indexOf('}', j);
            if (a < 0 || b < 0 || a > b) break;
            String obj = s.substring(a + 1, b);
            Map<String, Object> m = new HashMap<>();
            m.put("op", str(obj, "op"));
            m.put("name", str(obj, "name"));
            m.put("num", num(obj, "num"));
            Boolean cp = bool(obj, "callParent");
            if (cp != null) m.put("callParent", cp);
            Boolean pp = bool(obj, "passToParent");
            if (pp != null) m.put("passToParent", pp);
            out.add(m);
            j = b + 1;
        }
        return out;
    }

    static String str(String obj, String key) {
        int k = obj.indexOf('"' + key + '"');
        if (k < 0) return "";
        int c = obj.indexOf(':', k);
        int a = obj.indexOf('"', c + 1);
        int b = obj.indexOf('"', a + 1);
        if (a < 0 || b < 0) return "";
        return obj.substring(a + 1, b);
    }

    static Integer num(String obj, String key) {
        int k = obj.indexOf('"' + key + '"');
        if (k < 0) return 0;
        int c = obj.indexOf(':', k);
        int a = c + 1;
        while (a < obj.length() && !Character.isDigit(obj.charAt(a)) && obj.charAt(a) != '-') a++;
        int b = a;
        while (b < obj.length() && (Character.isDigit(obj.charAt(b)) || obj.charAt(b) == '-')) b++;
        if (b <= a) return 0;
        return Integer.parseInt(obj.substring(a, b));
    }

    static Boolean bool(String obj, String key) {
        int k = obj.indexOf('"' + key + '"');
        if (k < 0) return null;
        int c = obj.indexOf(':', k);
        String rest = obj.substring(c + 1).trim();
        if (rest.startsWith("true")) return Boolean.TRUE;
        if (rest.startsWith("false")) return Boolean.FALSE;
        return null;
    }
}
