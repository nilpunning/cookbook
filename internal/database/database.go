package database

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func Setup() *sql.DB {
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE recipe (
			filename TEXT NOT NULL PRIMARY KEY,
			name TEXT NOT NULL,
			webpath TEXT NOT NULL,
			html TEXT NOT NULL
		);
		CREATE INDEX idx_recipe_webpath ON recipe (webpath);
		CREATE TABLE recipe_tag (
			recipe_filename TEXT NOT NULL,
			tag_name TEXT NOT NULL,
			PRIMARY KEY (recipe_filename, tag_name)
		);
	`)
	if err != nil {
		log.Fatal(err)
	}
	return db
}

func UpsertRecipe(db *sql.DB, filename, name, webpath, html string, tags []string) error {
	tx, err := db.Begin()
	if err != nil {
		log.Println("Error beginning transaction:", err)
		return err
	}
	if _, err := tx.Exec(`
			INSERT INTO recipe (filename, name, webpath, html)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(filename) DO UPDATE SET name = ?, webpath = ?, html = ?;
			DELETE FROM recipe_tag WHERE recipe_filename = ?
		`,
		filename, name, webpath, html,
		name, webpath, html,
		filename); err != nil {
		log.Printf("Error upserting recipe %s: %v", filename, err)
		tx.Rollback()
		return err
	}

	for _, tag := range tags {
		if _, err := tx.Exec(`
			INSERT INTO recipe_tag (recipe_filename, tag_name) VALUES (?, ?)
			ON CONFLICT(recipe_filename, tag_name) DO NOTHING
		`, filename, tag); err != nil {
			log.Printf("Error upserting recipe tag %s: %v", tag, err)
			tx.Rollback()
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction for recipe %s: %v", filename, err)
		return err
	}
	return nil
}

func DeleteRecipe(db *sql.DB, path string) error {
	if _, err := db.Exec(`
		DELETE FROM recipe WHERE filename = ?
	`, path); err != nil {
		log.Printf("Error deleting recipe %s: %v", path, err)
		return err
	}
	return nil
}

func GetRecipe(db *sql.DB, webpath string) (string, string, error) {
	row := db.QueryRow(`
		SELECT name, html FROM recipe WHERE webpath = ?
	`, webpath)

	var name, html string
	return name, html, row.Scan(&name, &html)
}

type RecipesGroupedByTag struct {
	Tag     string
	Recipes []map[string]string
}

func GetRecipesGroupedByTag(db *sql.DB) ([]RecipesGroupedByTag, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Commit()

	rows, err := tx.Query(`
		SELECT recipe_tag.tag_name, recipe.name, recipe.webpath FROM recipe_tag
		JOIN recipe ON recipe_tag.recipe_filename = recipe.filename
		ORDER BY recipe_tag.tag_name, recipe.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []RecipesGroupedByTag
	var prevTag string
	for rows.Next() {
		var tag_name, name, webpath string
		err := rows.Scan(&tag_name, &name, &webpath)
		if err != nil {
			return nil, err
		}
		if tag_name != prevTag {
			tags = append(tags, RecipesGroupedByTag{Tag: tag_name, Recipes: []map[string]string{}})
			prevTag = tag_name
		}
		last := &tags[len(tags)-1]
		last.Recipes = append(last.Recipes, map[string]string{"Name": name, "Webpath": webpath})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return tags, nil
}
