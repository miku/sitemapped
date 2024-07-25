// sitemapped turns a single URL pointing to a sitemap into a list of URL,
// resolving sitemapindex type sitemaps, too.
//
// $ sitemapped https://core.ac.uk/sitemap.xml
//
// Some sitemap index style sitemaps may point to thousands of actual sitemaps.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"crypto/tls"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/sethgrid/pester"
	"golang.org/x/net/html/charset"
)

const Version = "0.1.5"

// SitemapIndexEntry is an entry in a sitemap index style sitemap.
type SitemapIndexEntry struct {
	XMLName xml.Name `xml:"sitemap"`
	Text    string   `xml:",chardata"`
	Loc     string   `xml:"loc"`     // https://core.ac.uk/sitema...
	Lastmod string   `xml:"lastmod"` // 2021-01-08, 2021-01-08, 2...
}

// Sitemapindex was generated 2024-07-01 15:50:15 by tir on reka with zek 0.1.24.
type Sitemapindex struct {
	XMLName xml.Name            `xml:"sitemapindex"`
	Text    string              `xml:",chardata"`
	Xmlns   string              `xml:"xmlns,attr"`
	Sitemap []SitemapIndexEntry `xml:"sitemap"`
}

// Urlset was generated 2024-07-01 20:25:25 by tir on reka with zek 0.1.24.
type Urlset struct {
	XMLName xml.Name `xml:"urlset"`
	Text    string   `xml:",chardata"`
	Xmlns   string   `xml:"xmlns,attr"`
	URL     []struct {
		Text string `xml:",chardata"`
		Loc  string `xml:"loc"` // https://core.ac.uk/displa...
	} `xml:"url"`
}

var (
	defaultCachePath = path.Join(xdg.CacheHome, "sitemap")

	maxRetries  = flag.Int("r", 3, "max HTTP client retries")
	cacheDir    = flag.String("cache-dir", defaultCachePath, "path to cache directory")
	force       = flag.Bool("f", false, "force redownload, even if cached file exists")
	showVersion = flag.Bool("version", false, "show version")
	timeout     = flag.Duration("T", 15*time.Second, "timeout")
	userAgent   = flag.String("ua", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", "user agent")
)

func main() {
	flag.Parse()
	if *showVersion {
		fmt.Println(Version)
		os.Exit(0)
	}
	if flag.NArg() == 0 {
		log.Fatal("a sitemap.xml URL is required")
	}
	if err := os.MkdirAll(*cacheDir, 755); err != nil {
		log.Fatal(err)
	}
	transport := http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   *timeout,
		Transport: &transport,
	}
	httpClient := pester.NewExtendedClient(client)
	httpClient.MaxRetries = *maxRetries
	httpClient.Backoff = pester.ExponentialBackoff
	httpClient.RetryOnHTTP429 = true
	cache := &Cache{Client: httpClient, Dir: *cacheDir, UserAgent: *userAgent}
	sitemapURL := flag.Arg(0) // sitemap or sitemapindex
	fn, err := cache.URL(sitemapURL, nil)
	if err != nil {
		log.Fatal(err)
	}
	isIndex, err := isSitemapIndex(fn)
	if err != nil {
		log.Fatal(err)
	}
	f, err := os.Open(fn)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()
	if isIndex {
		err = urlsFromSitemapIndex(cache, f, bw)
	} else {
		err = urlsFromSitemap(f, bw)
	}
	if err != nil {
		log.Fatal(err)
	}
}

// isSitemapIndex returns true if this an index.
func isSitemapIndex(filename string) (bool, error) {
	f, err := os.Open(filename)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, 1024)
	_, err = f.Read(buf)
	if err != nil {
		return false, err
	}
	return bytes.Contains(buf, []byte("sitemapindex")), nil
}

func urlsFromSitemapIndex(cache *Cache, r io.Reader, w io.Writer) error {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = charset.NewReaderLabel
	var smi Sitemapindex
	err := dec.Decode(&smi)
	if err != nil {
		return err
	}
	for _, sm := range smi.Sitemap {
		fn, err := cache.URL(sm.Loc, &DownloadOpts{Force: *force})
		if err != nil {
			return err
		}
		// No defer for closeList, as we are exiting the program anyway, if we
		// fail here. If that is to be changed, add a defer.
		var closeList []io.Closer
		var rc io.ReadCloser
		f, err := os.Open(fn)
		if err != nil {
			return err
		}
		closeList = append(closeList, f)
		switch {
		case strings.HasSuffix(sm.Loc, ".gz"):
			rc, err = gzip.NewReader(f)
			if err != nil {
				return err
			}
			closeList = append(closeList, rc)
		default:
			rc = f
		}
		dec = xml.NewDecoder(rc)
		dec.CharsetReader = charset.NewReaderLabel
		var uset Urlset
		if err := dec.Decode(&uset); err != nil {
			log.Fatal(err)
		}
		for _, u := range uset.URL {
			_, err := fmt.Fprintln(w, strings.TrimSpace(u.Loc))
			if err != nil {
				return err
			}
		}
		slices.Reverse(closeList) // close gz RC before file
		for _, c := range closeList {
			if err := c.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

func urlsFromSitemap(r io.Reader, w io.Writer) error {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = charset.NewReaderLabel
	var urlset Urlset
	err := dec.Decode(&urlset)
	if err != nil {
		return err
	}
	for _, u := range urlset.URL {
		_, err := fmt.Fprintln(w, strings.TrimSpace(u.Loc))
		if err != nil {
			return err
		}
	}
	return nil
}

type Cache struct {
	Dir       string
	Client    Doer
	UserAgent string
}

type DownloadOpts struct {
	Filename string // a specific filename to use, if any
	Force    bool   // attempt redownload in any case
}

// URL returns the path to cached file for a given URL. If force is true,
// redownload, even if copy exists.
func (c *Cache) URL(url string, opts *DownloadOpts) (string, error) {
	dir := c.Dir
	if opts == nil || opts.Filename == "" {
		h := sha1.New()
		_, _ = h.Write([]byte(url))
		digest := fmt.Sprintf("%x", h.Sum(nil))
		shard := digest[:2]
		opts = &DownloadOpts{Filename: digest}
		dir = path.Join(c.Dir, shard)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
	}
	dst := path.Join(dir, opts.Filename)
	if _, err := os.Stat(dst); os.IsNotExist(err) || opts.Force {
		if err := DownloadFile(c.Client, url, dst, c.UserAgent); err != nil {
			return "", err
		}
	}
	return dst, nil
}

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// DownloadFile retrieves a file from URL, atomically.
func DownloadFile(client Doer, url string, dst string, userAgent string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// tempfile, same path, so assume save to atomically rename(2).
	tmpf := dst + ".wip"
	f, err := os.OpenFile(tmpf, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmpf, dst)
}
