package effects

func init() {
	RegisterNonAPI(
		"kw:Prevent all combat damage that would be dealt to CARDNAME.",
		"kw:Prevent all combat damage that would be dealt to and dealt by CARDNAME.",
		"kw:Prevent all damage that would be dealt to CARDNAME.",
	)
}
