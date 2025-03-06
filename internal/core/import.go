package core

import (
	"fmt"
	"os"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

func ExtractTextFromHTMLFile(filename string) error {
	fmt.Println(filename)

	// Open the file
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	// Parse the HTML
	doc, err := html.Parse(file)
	if err != nil {
		return fmt.Errorf("error parsing HTML: %v", err)
	}

	// Extract and print text
	var extract func(*html.Node, bool)
	extract = func(n *html.Node, inBaddie bool) {
		if n.Type == html.TextNode {
			if !inBaddie {
				text := strings.TrimSpace(n.Data)
				if text != "" {
					fmt.Println("===>", text)
				}
			}
		} else if n.Data == "noscript" {
			// https://github.com/golang/go/issues/16318
			fmt.Println(n.Type, n.Data)
			if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
				doc, err := html.Parse(strings.NewReader(n.FirstChild.Data))
				if err == nil {
					extract(doc, false)
				}
			}
		} else {
			fmt.Println(n.Type, n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c, inBaddie || n.Data == "script" || n.Data == "style")
		}
	}

	extract(doc, false)
	return nil
}

func StripExtraneousHTML(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	doc, err := goquery.NewDocumentFromReader(file)
	if err != nil {
		return fmt.Errorf("error parsing HTML: %v", err)
	}

	// https://github.com/PuerkitoBio/goquery/issues/139
	doc.Find("noscript").Each(func(i int, selection *goquery.Selection) {
		selection.ReplaceWithHtml(selection.Text())
	}).End()

	doc.Find("head,script,style,link,svg").Remove().End()

	doc.Contents().FilterFunction(func(i int, s *goquery.Selection) bool {
		return s.Get(0).Type == html.CommentNode
	}).Remove().End()

	clean := func(i int, s *goquery.Selection) {
		if n := s.Get(0); n != nil {
			n.Data = strings.TrimSpace(n.Data)
			n.Attr = nil
		}
	}

	doc.Find("*").Each(clean)
	doc.Find("*").Contents().Each(clean)

	html, err := doc.Html()
	if err != nil {
		return fmt.Errorf("error getting HTML: %v", err)
	}
	fmt.Println(html)

	return nil
}
