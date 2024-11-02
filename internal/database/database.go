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
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			filename TEXT NOT NULL UNIQUE,
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
		CREATE VIRTUAL TABLE recipe_fts USING fts5(
			name,
			html,
			content='recipe',
			content_rowid='id',
			tokenize='porter unicode61'
		);
		CREATE TRIGGER recipe_ai AFTER INSERT ON recipe BEGIN
			INSERT INTO recipe_fts(rowid, name, html)
			VALUES (new.id, new.name, new.html);
		END;
		CREATE TRIGGER recipe_ad AFTER DELETE ON recipe BEGIN
			INSERT INTO recipe_fts(recipe_fts, rowid, name, html)
			VALUES('delete', old.id, old.name, old.html);
		END;
		CREATE TRIGGER recipe_au AFTER UPDATE ON recipe BEGIN
			INSERT INTO recipe_fts(recipe_fts, rowid, name, html)
			VALUES('delete', old.id, old.name, old.html);
			INSERT INTO recipe_fts(rowid, name, html)
			VALUES (new.id, new.name, new.html);
		END;
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
	TagName string
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
	for rows.Next() {
		var tag_name, name, webpath string
		err := rows.Scan(&tag_name, &name, &webpath)
		if err != nil {
			return nil, err
		}
		if len(tags) == 0 || tag_name != tags[len(tags)-1].TagName {
			tags = append(tags, RecipesGroupedByTag{TagName: tag_name, Recipes: []map[string]string{}})
		}
		currentTag := &tags[len(tags)-1]
		currentTag.Recipes = append(
			currentTag.Recipes,
			map[string]string{"Name": name, "Webpath": webpath},
		)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return tags, nil
}

type SearchResult struct {
	Name    string
	Webpath string
}

func SearchRecipes(db *sql.DB, query string) ([]SearchResult, error) {
	rows, err := db.Query(`
		SELECT r.name, r.webpath
		FROM recipe r
		JOIN recipe_fts fts ON r.id = fts.rowid
		WHERE recipe_fts MATCH ?
		ORDER BY r.name
	`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var result SearchResult

		if err := rows.Scan(&result.Name, &result.Webpath); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}
