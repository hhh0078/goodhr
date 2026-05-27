// 自动迁移
package httpapi
import ("database/sql"; "log"; "os"; "path/filepath"; "sort"; "strings")

func RunMigrations(db *sql.DB) error {
	d := "db/migrations"; es,_:=os.ReadDir(d); var fs []string
	for _,e:=range es { n:=e.Name(); if !e.IsDir() && strings.HasSuffix(n,".sql") && !strings.HasSuffix(n,".down.sql") { fs=append(fs, n) } }
	sort.Strings(fs)
	for _,f:=range fs { b,_:=os.ReadFile(filepath.Join(d,f)); log.Println("[migrate]",f); if _,err:=db.Exec(string(b));err!=nil{return err} }
	log.Println("[migrate] done"); return nil
}
