package libcats

type redditResponseDto struct {
	Kind string
	Data redditCollectionDto
}

type redditCollectionDto struct {
	Modhash  string
	Children []redditPostDto1
}

type redditPostDto1 struct {
	Kind string
	Data redditPostDto
}

type redditPostDto struct {
	Url       string
	Name      string
	Title     string
	Author    string
	Permalink string
}
