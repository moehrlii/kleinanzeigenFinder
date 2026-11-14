package immoscout

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"maps"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"github.com/Kamuyin/kleinanzeigenFinder/providers/parsing"
)

const (
	immoScoutWebBaseURL = "https://www.immobilienscout24.de/expose/"
)

var immoScoutAllowedParams = map[string]string{
	"apartmenttypes":          "apartmenttypes",
	"buildingtypes":           "buildingtypes",
	"constructionyear":        "constructionyear",
	"energyefficiencyclasses": "energyefficiencyclasses",
	"equipment":               "equipment",
	"exclusioncriteria":       "exclusioncriteria",
	"floor":                   "floor",
	"fulltext":                "fulltext",
	"geocodes":                "geocodes",
	"geocoordinates":          "geocoordinates",
	"ground":                  "ground",
	"haspromotion":            "haspromotion",
	"heatingtypes":            "heatingtypes",
	"livingspace":             "livingspace",
	"newbuilding":             "newbuilding",
	"numberofrooms":           "numberofrooms",
	"petsallowedtypes":        "petsallowedtypes",
	"price":                   "price",
	"pricetype":               "pricetype",
	"sorting":                 "sorting",
}

var immoScoutRealEstateTypes = map[string]string{
	"haus-mieten":                       "houserent",
	"haus-kaufen":                       "housebuy",
	"mietwohnungen":                     "apartmentrent",
	"mietwohnungen-mit-tauschwohnungen": "apartmentrent",
	"wohnung-kaufen":                    "apartmentbuy",
	"wohnung-mieten":                    "apartmentrent",
	"wohnung-kaufen-mit-balkon":         "apartmentbuy",
	"eigentumswohnung-mit-garten":       "apartmentbuy",
}

var immoScoutPathParams = map[string]map[string][]string{
	"barrierefreie-wohnung-mieten":    {"equipment": []string{"handicappedAccessible"}},
	"dachgeschosswohnung-mieten":      {"apartmenttypes": []string{"roofstorey"}},
	"erdgeschosswohnung-mieten":       {"apartmenttypes": []string{"groundfloor"}},
	"etagenwohnung-mieten":            {"apartmenttypes": []string{"apartment"}},
	"hochparterrewohnung-mieten":      {"apartmenttypes": []string{"raisedgroundfloor"}},
	"loft-mieten":                     {"apartmenttypes": []string{"loft"}},
	"maisonette-mieten":               {"apartmenttypes": []string{"maisonette"}},
	"neubauwohnung-mieten":            {"newbuilding": []string{"true"}},
	"penthousewohnung-mieten":         {"apartmenttypes": []string{"penthouse"}},
	"penthouse-mieten":                {"apartmenttypes": []string{"penthouse"}},
	"souterrainwohnung-mieten":        {"apartmenttypes": []string{"halfbasement"}},
	"terrassenwohnung-mieten":         {"apartmenttypes": []string{"terracedflat"}},
	"wohnung-mit-balkon-mieten":       {"equipment": []string{"balcony"}},
	"wohnung-mit-einbaukueche-mieten": {"equipment": []string{"builtInKitchen"}},
	"wohnung-mit-garage-mieten":       {"equipment": []string{"parking"}},
	"wohnung-mit-garten-mieten":       {"equipment": []string{"garden"}},
	"wohnung-mit-keller-mieten":       {"equipment": []string{"cellar"}},
}

var immoScoutEquipmentMap = map[string]string{
	"balcony":               "balcony",
	"builtinkitchen":        "builtInKitchen",
	"cellar":                "cellar",
	"garden":                "garden",
	"guesttoilet":           "guestToilet",
	"handicappedaccessible": "handicappedAccessible",
	"lift":                  "lift",
	"lodgerflat":            "lodgerflat",
	"parking":               "parking",
}

var immoScoutSortingMap = map[string]string{
	"2": "-firstactivation",
	"3": "-price",
	"4": "price",
	"5": "-rooms",
	"6": "rooms",
	"7": "-livingspace",
	"8": "livingspace",
}

var immoScoutListParams = map[string]bool{
	"apartmenttypes":          true,
	"buildingtypes":           true,
	"energyefficiencyclasses": true,
	"equipment":               true,
	"exclusioncriteria":       true,
	"heatingtypes":            true,
	"petsallowedtypes":        true,
}

type ImmoScout struct {
	settings config.ImmoScoutSettings
	client   *http.Client
}

func New(settings config.ImmoScoutSettings) *ImmoScout {
	if settings.APIBaseURL == "" {
		settings.APIBaseURL = "https://api.mobile.immobilienscout24.de"
	}
	if settings.UserAgent == "" {
		settings.UserAgent = "ImmoScout_27.12_26.2_._"
	}
	if settings.PageSize <= 0 {
		settings.PageSize = 50
	}

	return &ImmoScout{
		settings: settings,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *ImmoScout) Close() error {
	s.client.CloseIdleConnections()
	return nil
}

func (s *ImmoScout) FetchListingsPages(searchURL string, maxResults, maxPages int) ([]model.Listing, error) {
	apiURL, err := s.translateSearchURL(searchURL)
	if err != nil {
		return nil, err
	}

	pageNumber := 1
	var listings []model.Listing
	seen := make(map[string]bool)
	resultCount := 0

	for {
		query := apiURL.Query()
		query.Set("pagenumber", strconv.Itoa(pageNumber))
		query.Set("pagesize", strconv.Itoa(s.settings.PageSize))
		apiURL.RawQuery = query.Encode()

		response, err := s.fetchList(apiURL.String())
		if err != nil {
			return listings, err
		}
		pageListings := s.listingsFromResponse(response)
		resultCount += len(response.ResultListItems)
		for _, listing := range pageListings {
			if !seen[listing.ID] {
				seen[listing.ID] = true
				listings = append(listings, listing)
			}
		}

		if maxResults > 0 && len(listings) >= maxResults {
			return listings[:maxResults], nil
		}
		if len(response.ResultListItems) == 0 {
			return listings, nil
		}
		if maxPages > 0 && pageNumber >= maxPages {
			return listings, nil
		}
		if response.TotalResults > 0 && resultCount >= response.TotalResults {
			return listings, nil
		}
		if len(response.ResultListItems) < s.settings.PageSize {
			return listings, nil
		}

		pageNumber++
	}
}

func (s *ImmoScout) FetchDetail(listing model.Listing) (model.Listing, error) {
	exposeID := immoScoutExposeID(listing)
	if exposeID == "" {
		return listing, fmt.Errorf("could not determine ImmoScout expose ID")
	}

	detailURL, err := url.JoinPath(s.settings.APIBaseURL, "expose", exposeID)
	if err != nil {
		return listing, err
	}

	req, err := http.NewRequest(http.MethodGet, detailURL, nil)
	if err != nil {
		return listing, err
	}
	s.setHeaders(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return listing, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return listing, fmt.Errorf("ImmoScout detail API returned HTTP %d", resp.StatusCode)
	}

	var payload immoScoutExposeResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return listing, err
	}

	listing.ImageURL = firstNonEmpty(imageGallery(listing.ImageURL)...)
	listing.ImageURLs = imageGallery(listing.ImageURLs...)
	detail := s.detailListingFromResponse(exposeID, listing, payload)
	detail.DetailLoaded = true
	return listing.MergedWith(detail), nil
}

func (s *ImmoScout) translateSearchURL(rawURL string) (*url.URL, error) {
	if err := validateSearchHost(rawURL); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(parsed.Hostname(), "api.mobile.immobilienscout24.de") {
		if parsed.Path != "/search/list" {
			return nil, fmt.Errorf("expected ImmoScout /search/list API URL")
		}
		endpoint, err := url.Parse(s.settings.APIBaseURL)
		if err != nil {
			return nil, err
		}
		endpoint.Path = path.Join(endpoint.Path, "search", "list")
		endpoint.RawQuery = parsed.RawQuery
		return endpoint, nil
	}

	segments := pathSegments(parsed.Path)
	if len(segments) < 3 || segments[0] != "Suche" {
		return nil, fmt.Errorf("unsupported ImmoScout search URL path: %s", parsed.Path)
	}
	if slices.Contains(segments, "shape") {
		return nil, fmt.Errorf("ImmoScout shape searches are not supported yet")
	}

	searchType := "region"
	if slices.Contains(segments, "radius") {
		searchType = "radius"
	}

	realEstateKey := segments[len(segments)-1]
	realEstateType := immoScoutRealEstateTypes[realEstateKey]
	if realEstateType == "" {
		if _, ok := immoScoutPathParams[realEstateKey]; ok {
			realEstateType = "apartmentrent"
		} else {
			return nil, fmt.Errorf("unsupported ImmoScout real estate type: %s", realEstateKey)
		}
	}

	apiURL, err := url.Parse(s.settings.APIBaseURL)
	if err != nil {
		return nil, err
	}
	apiURL.Path = path.Join(strings.TrimRight(apiURL.Path, "/"), "search", "list")

	query := url.Values{}
	query.Set("realestatetype", realEstateType)
	query.Set("searchType", searchType)
	query.Set("sorting", "-firstactivation")

	if searchType == "region" {
		query.Set("geocodes", "/"+strings.Join(segments[1:len(segments)-1], "/"))
	}

	for key, values := range parseQueryAmpOnly(parsed.RawQuery) {
		mappedKey, ok := immoScoutAllowedParams[strings.ToLower(key)]
		if !ok {
			continue
		}
		for _, value := range splitCommaValues(values) {
			addImmoScoutParam(query, mappedKey, value)
		}
	}

	for key, values := range immoScoutPathParams[realEstateKey] {
		for _, value := range values {
			addImmoScoutParam(query, key, value)
		}
	}

	if sorting := query.Get("sorting"); sorting != "" {
		if mapped, ok := immoScoutSortingMap[sorting]; ok {
			query.Set("sorting", mapped)
		}
	}

	apiURL.RawQuery = query.Encode()
	return apiURL, nil
}

func (s *ImmoScout) fetchList(apiURL string) (immoScoutListResponse, error) {
	body := []byte(`{"supportedResultListType":[],"supportedResultListTypes":[],"userData":{}}`)
	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return immoScoutListResponse{}, err
	}
	s.setHeaders(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return immoScoutListResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return immoScoutListResponse{}, fmt.Errorf("ImmoScout list API returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	var payload immoScoutListResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return immoScoutListResponse{}, err
	}
	return payload, nil
}

func (s *ImmoScout) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", s.settings.UserAgent)
}

func (s *ImmoScout) listingsFromResponse(payload immoScoutListResponse) []model.Listing {
	listings := make([]model.Listing, 0, len(payload.ResultListItems))
	for _, result := range payload.ResultListItems {
		if result.Type != "EXPOSE_RESULT" || result.Item.ID == "" {
			continue
		}
		listing := listingFromImmoScoutItem(result.Item)
		listings = append(listings, listing)
	}
	return listings
}

func listingFromImmoScoutItem(item immoScoutListItem) model.Listing {
	attributes := make(map[string]string)
	for index, attr := range item.Attributes {
		key := immoScoutAttributeKey(attr.Label, attr.Value, index)
		if key != "" && attr.Value != "" {
			attributes[key] = normalizeText(attr.Value)
		}
	}
	if item.RealEstateType != "" {
		attributes["Objektart"] = item.RealEstateType
	}
	if item.PaywallListing.Active {
		attributes["ImmoScout Paywall"] = item.PaywallListing.Text
	}
	if item.ListOnlyOnIS24 {
		attributes["Nur auf ImmoScout24"] = "true"
	}
	if item.IsPrivate {
		attributes["Anbieter"] = "Privat"
	} else if item.Realtor.LogoURLScale != "" {
		attributes["Anbieter"] = "Gewerblich"
	}

	price := firstNonEmpty(attributes["Warmmiete"], attributes["Kaltmiete"], attributes["Preis"])
	imageURL := firstNonEmpty(item.TitlePicture.Full, item.TitlePicture.Preview)
	rawText := buildRawText(item.Title, price, item.Address.Line, item.Published, attributes)

	return model.Listing{
		Platform:    ID,
		ID:          "is24:" + item.ID,
		URL:         immoScoutWebBaseURL + item.ID,
		Title:       normalizeText(item.Title),
		Price:       price,
		Location:    normalizeText(item.Address.Line),
		PostedAt:    normalizeText(item.Published),
		ImageURL:    firstNonEmpty(imageGallery(imageURL)...),
		ImageURLs:   imageGallery(imageURL),
		RawText:     rawText,
		PriceAmount: parsing.ParsePriceAmount(normalizeText(price)),
		Attributes:  attributes,
	}
}

func (s *ImmoScout) detailListingFromResponse(exposeID string, base model.Listing, payload immoScoutExposeResponse) model.Listing {
	attributes := maps.Clone(base.Attributes)
	if attributes == nil {
		attributes = make(map[string]string)
	}
	var descriptionParts []string
	var rawParts []string
	var imageURLs []string

	for _, section := range payload.Sections {
		switch section.Type {
		case "MEDIA":
			for _, media := range section.Media {
				imageURLs = append(imageURLs, firstNonEmpty(media.FullImageURL, media.PreviewImageURL))
				if media.Caption != "" {
					rawParts = append(rawParts, media.Caption)
				}
			}
		case "TITLE":
			if section.Title != "" {
				rawParts = append(rawParts, section.Title)
			}
		case "TEXT_AREA":
			text := joinNonEmpty("\n", section.Title, section.Text)
			if text != "" {
				descriptionParts = append(descriptionParts, text)
				rawParts = append(rawParts, text)
			}
		default:
			if section.Title != "" && section.Text != "" {
				rawParts = append(rawParts, section.Title+" "+section.Text)
			}
		}

		for _, attr := range section.Attributes {
			if attr.Label == "" || attr.Text == "" {
				continue
			}
			key := normalizeText(attr.Label)
			value := normalizeText(attr.Text)
			attributes[key] = value
			rawParts = append(rawParts, key+" "+value)
		}

		if section.Company != "" {
			attributes["Anbieter Firma"] = normalizeText(section.Company)
		}
		if section.Name != "" {
			attributes["Anbieter Name"] = normalizeText(section.Name)
		}
	}

	if payload.Contact.ContactData.Agent.Name != "" {
		attributes["Anbieter Name"] = normalizeText(payload.Contact.ContactData.Agent.Name)
	}
	if payload.Contact.ContactData.Agent.Company != "" {
		attributes["Anbieter Firma"] = normalizeText(payload.Contact.ContactData.Agent.Company)
	}

	seller := firstNonEmpty(attributes["Anbieter Name"], attributes["Anbieter Firma"], base.Seller)
	if seller != "" {
		rawParts = append(rawParts, "Anbieter "+seller)
	}

	images := imageGallery(imageURLs...)
	price := firstNonEmpty(attributes["Warmmiete"], attributes["Kaltmiete"], attributes["Preis"], base.Price)
	rawText := joinNonEmpty("\n", base.RawText, strings.Join(rawParts, "\n"))

	return model.Listing{
		Platform:    ID,
		ID:          "is24:" + exposeID,
		URL:         immoScoutWebBaseURL + exposeID,
		Title:       firstNonEmpty(base.Title, attributes["Titel"]),
		Price:       price,
		Location:    base.Location,
		PostedAt:    base.PostedAt,
		Description: strings.TrimSpace(strings.Join(descriptionParts, "\n\n")),
		ImageURL:    firstNonEmpty(append(slices.Clone(images), base.ImageURL)...),
		ImageURLs:   images,
		Seller:      seller,
		RawText:     rawText,
		PriceAmount: parsing.ParsePriceAmount(normalizeText(price)),
		Attributes:  attributes,
	}
}

func addImmoScoutParam(query url.Values, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	switch key {
	case "equipment":
		if mapped := immoScoutEquipmentMap[strings.ToLower(value)]; mapped != "" {
			query.Add(key, mapped)
		}
	case "exclusioncriteria":
		if strings.EqualFold(value, "swapflat") {
			query.Add(key, "swap_flat")
		} else {
			query.Add(key, value)
		}
	default:
		if immoScoutListParams[key] {
			query.Add(key, value)
		} else {
			query.Set(key, value)
		}
	}
}

func immoScoutAttributeKey(label, value string, index int) string {
	label = normalizeText(label)
	value = normalizeText(value)
	if label != "" {
		return label
	}
	switch index {
	case 0:
		return "Preis"
	case 1:
		return "Wohnfläche"
	case 2:
		return "Zimmer"
	}
	switch {
	case strings.Contains(value, "€"):
		if index == 0 {
			return "Preis"
		}
		return "Warmmiete"
	case strings.Contains(value, "m²") || strings.Contains(value, "m2"):
		return "Wohnfläche"
	case strings.Contains(strings.ToLower(value), "zi"):
		return "Zimmer"
	}
	return ""
}

func immoScoutExposeID(listing model.Listing) string {
	if strings.HasPrefix(listing.ID, "is24:") {
		return strings.TrimPrefix(listing.ID, "is24:")
	}
	parsed, err := url.Parse(listing.URL)
	if err != nil {
		return ""
	}
	return path.Base(parsed.Path)
}

func pathSegments(rawPath string) []string {
	raw := strings.Trim(rawPath, "/")
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "/")
}

func splitCommaValues(values []string) []string {
	var result []string
	for _, value := range values {
		for item := range strings.SplitSeq(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				result = append(result, item)
			}
		}
	}
	return result
}

func parseQueryAmpOnly(rawQuery string) url.Values {
	values := url.Values{}
	for pair := range strings.SplitSeq(rawQuery, "&") {
		if pair == "" {
			continue
		}
		key, value, _ := strings.Cut(pair, "=")
		decodedKey, err := url.QueryUnescape(key)
		if err != nil || decodedKey == "" {
			continue
		}
		decodedValue := ""
		if value != "" {
			decodedValue, err = url.QueryUnescape(value)
			if err != nil {
				continue
			}
		}
		values.Add(decodedKey, decodedValue)
	}
	return values
}

func normalizeText(value string) string {
	value = html.UnescapeString(value)
	value = strings.ReplaceAll(value, "\u00a0", " ")
	return strings.Join(strings.Fields(value), " ")
}

// imageGallery excludes placeholders and deduplicates images before AI assessment.
func imageGallery(values ...string) []string {
	var images []string
	for _, value := range values {
		value = cleanImmoScoutImageURL(value)
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.Host == "" || strings.Contains(u.Path, "/statpic/placeholder_") || slices.Contains(images, value) {
			continue
		}
		images = append(images, value)
	}
	return images
}

func cleanImmoScoutImageURL(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "%WIDTH%x%HEIGHT%", "800x600")
	return strings.ReplaceAll(value, "%WIDTH%x%HEIGHT%%3E", "800x600%3E")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return normalizeText(value)
		}
	}
	return ""
}

func joinNonEmpty(separator string, values ...string) string {
	var parts []string
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, normalizeText(value))
		}
	}
	return strings.Join(parts, separator)
}

func buildRawText(title, price, location, postedAt string, attributes map[string]string) string {
	parts := []string{title, price, location, postedAt}
	for key, value := range attributes {
		if key != "" && value != "" {
			parts = append(parts, key+" "+value)
		}
	}
	return joinNonEmpty("\n", parts...)
}

type immoScoutListResponse struct {
	TotalResults    int                       `json:"totalResults"`
	ResultListItems []immoScoutResultListItem `json:"resultListItems"`
}

type immoScoutResultListItem struct {
	Type string            `json:"type"`
	Item immoScoutListItem `json:"item"`
}

type immoScoutListItem struct {
	ID             string                   `json:"id"`
	Title          string                   `json:"title"`
	TitlePicture   immoScoutTitlePicture    `json:"titlePicture"`
	Address        immoScoutAddress         `json:"address"`
	Published      string                   `json:"published"`
	Attributes     []immoScoutCardAttribute `json:"attributes"`
	RealEstateType string                   `json:"realEstateType"`
	PaywallListing immoScoutPaywall         `json:"paywallListing"`
	IsPrivate      bool                     `json:"isPrivate"`
	ListOnlyOnIS24 bool                     `json:"listOnlyOnIs24"`
	Realtor        immoScoutRealtor         `json:"realtor"`
}

type immoScoutTitlePicture struct {
	Preview string `json:"preview"`
	Full    string `json:"full"`
}

type immoScoutAddress struct {
	Line string `json:"line"`
}

type immoScoutCardAttribute struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type immoScoutPaywall struct {
	Active bool   `json:"active"`
	Text   string `json:"text"`
}

type immoScoutRealtor struct {
	LogoURLScale string `json:"logoUrlScale"`
}

type immoScoutExposeResponse struct {
	Sections []immoScoutSection `json:"sections"`
	Contact  immoScoutContact   `json:"contact"`
}

type immoScoutSection struct {
	Type       string                     `json:"type"`
	Title      string                     `json:"title"`
	Text       string                     `json:"text"`
	Attributes []immoScoutDetailAttribute `json:"attributes"`
	Media      []immoScoutMedia           `json:"media"`
	Company    string                     `json:"company"`
	Name       string                     `json:"name"`
}

type immoScoutDetailAttribute struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

type immoScoutMedia struct {
	Caption         string `json:"caption"`
	PreviewImageURL string `json:"previewImageUrl"`
	FullImageURL    string `json:"fullImageUrl"`
}

type immoScoutContact struct {
	ContactData immoScoutContactData `json:"contactData"`
}

type immoScoutContactData struct {
	Agent immoScoutAgent `json:"agent"`
}

type immoScoutAgent struct {
	Name    string `json:"name"`
	Company string `json:"company"`
}
