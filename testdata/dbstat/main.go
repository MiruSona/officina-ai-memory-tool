package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", os.Args[1])
	if err != nil {
		panic(err)
	}
	for _, name := range []string{"auto_vacuum", "freelist_count", "page_count", "page_size"} {
		v := ""
		db.QueryRow("PRAGMA " + name).Scan(&v)
		fmt.Println(name, "=", v)
	}
	rows, err := db.Query("SELECT name, SUM(pgsize) FROM dbstat GROUP BY name ORDER BY 2 DESC")
	if err != nil {
		fmt.Println("dbstat 없음:", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		name := ""
		size := int64(0)
		rows.Scan(&name, &size)
		fmt.Printf("%-24s %8.2f MB\n", name, float64(size)/1e6)
	}
}
