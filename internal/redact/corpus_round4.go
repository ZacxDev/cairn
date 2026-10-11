package redact

// roundFourPlants are the shapes O15's rewrite exists for, one per mechanism, so the corpus
// asserts each mechanism's OWN rule fires: a line-anchored rule behind a grep prefix and behind a
// diff's `<`, and the key-context notations no per-format rule read. The values are lower-case and
// digits — invisible to the entropy rule — so only the named rule can catch each one.
func (g *gen) roundFourPlants(session string) {
	lower := "abcdefghijklmnopqrstuvwxyz0123456789"
	v := func() string { return g.pick(lower, 15) + "4" }

	// 74 — `.pgpass` read through a single-file `grep -n` (`N:`).
	g.record(g.toolResult(session, "3:db.example:5432:alpha:app:"+g.plant("grep-n-pgpass", "pgpass", v())+"\n", nil))

	// 75 — a Secret manifest in a diff's OLD side (`< `).
	g.record(g.toolResult(session, "1,6c1,6\n< apiVersion: v1\n< kind: Secret\n< metadata:\n<   name: alpha-db\n< data:\n<   replica: "+
		g.plant("diff-old-k8s-secret", "k8s-secret", v())+"\n---\n> apiVersion: v1\n", nil))

	// 76 — systemd `Environment=`; 77 — a long flag with a space; 78 — SQL `IDENTIFIED BY`.
	g.record(g.toolResult(session, "[Service]\nEnvironment=DB_PASSWORD="+g.plant("systemd-environment", "key-context", v())+"\n", nil))
	g.record(g.toolUse(session, "Bash", m{"command": "migrate --db-password " + g.plant("cli-long-flag", "key-context", v()) + " up"}))
	g.record(g.toolUse(session, "Bash", m{"command": "mysql -e \"CREATE USER 'app'@'%' IDENTIFIED BY '" +
		g.plant("sql-identified-by", "key-context", v()) + "'\""}))

	// 79 — `mysql -p<pw>`: a SHORT flag carries no name, so it is the cli-flag rule's.
	g.record(g.toolUse(session, "Bash", m{"command": "mysql -uroot -p" + g.plant("mysql-glued-p", "cli-flag", v()) + " alpha"}))
}
