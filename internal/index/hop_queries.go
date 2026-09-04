package index

// LinksAmong 은 준 id 들 **사이에만** 있는 링크다. 검색의 1-hop 가산이 쓴다 —
// 저장소 전체 그래프를 읽으면 20k 에서 예산 30ms 를 넘긴다 (설계 4절).
//
// 사람이 적은 links 와 기계가 찾은 auto_links 의 합집합이다 (결정 42 뒷정리).
// 검색에는 "같은 이야기의 다른 조각" 이라는 뜻만 쓰이므로 누가 이었는지는
// 상관없다. 가리는 자리는 gc 면제표뿐이고, 그쪽은 links 만 본다.
func (d *DB) LinksAmong(ids []string) (map[string]map[string]bool, error) {
	edges := map[string]map[string]bool{}
	if len(ids) == 0 {
		return edges, nil
	}
	marks := placeholders(len(ids))
	args := make([]any, 0, len(ids)*2)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, args...)
	args = append(args, args...)
	// auto_links 는 docid 쌍이라 id 로 되돌린다 (리뷰 B · V2). 상위 몇십 건만
	// 묻는 자리라 기본키 조회 두 번이 붙어도 값이 안 는다.
	rows, err := d.sql.Query(
		"SELECT src, dst FROM links WHERE src IN ("+marks+") AND dst IN ("+marks+")"+
			" UNION SELECT a.id, b.id FROM auto_links l"+
			" JOIN memories a ON a.docid = l.src JOIN memories b ON b.docid = l.dst"+
			" WHERE a.id IN ("+marks+") AND b.id IN ("+marks+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		src, dst := "", ""
		if err := rows.Scan(&src, &dst); err != nil {
			return nil, err
		}
		if edges[src] == nil {
			edges[src] = map[string]bool{}
		}
		edges[src][dst] = true
	}
	return edges, rows.Err()
}
