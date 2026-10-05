package index

// DocidsOf 는 기억 id 를 docid 로 바꾼다. 뜻 후보 섞기(C2)가 벡터 파일에서 데려온
// id 를 색인 행으로 잇는 자리다 — 벡터 파일은 id 로, 색인은 docid 로 센다.
// 색인에 없는 id 는 답에서 빠진다.
func (d *DB) DocidsOf(ids []string) (map[string]int64, error) {
	out := make(map[string]int64, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := d.sql.Query("SELECT id, docid FROM memories WHERE id IN ("+placeholders(len(ids))+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		id, docid := "", int64(0)
		if err := rows.Scan(&id, &docid); err != nil {
			return nil, err
		}
		out[id] = docid
	}
	return out, rows.Err()
}
