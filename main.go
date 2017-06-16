package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/garyburd/redigo/redis"
)

var pool = &redis.Pool{
	Dial: func() (redis.Conn, error) {
		return redis.Dial("tcp", "localhost:6379")
	},
}

func main() {
	http.HandleFunc("/", index)
	http.HandleFunc("/post/", getPost)
	http.HandleFunc("/create", createPost)
	http.ListenAndServe(":8080", nil)
}

type post struct {
	ID        int
	Link      string
	Title     string
	Body      string
	CreatedAt time.Time
}

func index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// list all posts
	c := pool.Get()
	defer c.Close()

	list, _ := redis.Ints(c.Do("ZREVRANGE", "post:created_at", 0, -1))
	for _, id := range list {
		c.Send("HGET", "post", id)
	}
	err := c.Flush()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	posts := make([]post, len(list))
	for i := range list {
		bs, _ := redis.Bytes(c.Receive())
		gob.NewDecoder(bytes.NewReader(bs)).Decode(&posts[i])
	}

	t := template.Must(template.New("").Parse(`
		<h1>Post list ({{.Count}})</h1>
		<a href="/create">Create new post</a><br>

		<ul>
		{{range .Posts}}
			<li><a href="/post/{{.Link}}">{{.Title}}</a></li>
		{{end}}
		</ul>
	`))

	w.Header().Set("Content-Type", "text/html")
	data := struct {
		Count int
		Posts []post
	}{len(posts), posts}
	t.Execute(w, &data)
}

func getPost(w http.ResponseWriter, r *http.Request) {
	link := strings.TrimPrefix(r.URL.Path, "/post/")

	c := pool.Get()
	defer c.Close()

	id, err := redis.Int(c.Do("HGET", "link", link))
	if err == redis.ErrNil {
		fmt.Fprintf(w, "post %s not found D:", link)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	bs, err := redis.Bytes(c.Do("HGET", "post", id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var p post
	err = gob.NewDecoder(bytes.NewReader(bs)).Decode(&p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "%s\n%s", p.Title, p.Body)
}

func createPost(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		p := post{
			Title:     r.FormValue("title"),
			Link:      r.FormValue("link"),
			Body:      r.FormValue("body"),
			CreatedAt: time.Now(),
		}

		c := pool.Get()
		defer c.Close()
		p.ID, _ = redis.Int(c.Do("INCR", "id:post"))
		buf := bytes.Buffer{}
		gob.NewEncoder(&buf).Encode(&p)

		c.Send("MULTI")
		c.Send("HSET", "post", p.ID, buf.Bytes())
		c.Send("ZADD", "post:created_at", p.CreatedAt.UnixNano(), p.ID)
		c.Send("HSET", "link", p.Link, p.ID)
		_, err := c.Do("EXEC")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/post/"+p.Link, http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `
		<h2>Create new Post</h2>
		<form method=POST>
			<label>Title</label>
			<input name=title>
			<br>
			<label>Link</label>
			<input name=link>
			<br>
			<label>Body</label>
			<textarea name=body></textarea>
			<br>
			<button type=submit>Create</button>
		</form>
	`)
}
