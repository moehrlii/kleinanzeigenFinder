package ebay

import _ "embed"

//go:embed search.js
var SearchExtractScript string

//go:embed detail.js
var DetailExtractScript string
