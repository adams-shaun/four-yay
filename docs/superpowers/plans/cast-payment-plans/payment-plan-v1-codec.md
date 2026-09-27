# Payment-plan V1 identity codec

V1 identities are the lower-case, full 32-byte SHA-256 digest of a map-free
binary stream. Integers are unsigned big-endian (`u32` or `u64`); strings are
their `u32` byte length followed by UTF-8 bytes. The fixed mana vector is six
`u32` values in W/U/B/R/G/C order.

An action hashes:

```text
string("gorge.payment-action.v1"), u32(version), u64(seq), u32(player),
u64(cast.object), u64(cast.face), string(cast.origin)
```

A plan prefixes the same cast fields with
`string("gorge.payment-plan.v1")`, then appends its generic cost, mana cost,
activation count, each activation (`source`, `source_zone_seq`, ability kind,
face, index, intrinsic name, produces), then `pool_spend` and `pool_after`.
The plan `id` field, labels, preferred rank, wrapper list order and
`base_option_index` are excluded. Activations retain execution order.

`decision.TestPaymentPlanIdentityIsIndependentOfPresentation` pins this V1
fixture: action `7cd7d94c50da5e37389ac770de705e044430bd2ba4b8067202b36fe812f39ee4`
and plan `3e5670814bfa0bd0d71551dd79dcbbaa8b04212bd52bbfc8305a68ba5dbbb5f1`.
