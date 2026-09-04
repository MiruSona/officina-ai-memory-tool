package quality

// 임베딩 — 중복 1단의 **셋째 신호** 자리 (설계 결정 15·30).
//
// 후보 뽑기는 ① 문장 조각 겹침 ② simhash 밴딩 ③ 임베딩 코사인 셋의 합집합이다.
// ③ 은 물결 3(3A)이 채운다. **여기는 자리만 만들어 둔다** — 없으면 그냥 건너뛴다.
//
// 0실측 T0 이 「임베딩이 진짜로 필요한 건 12건 중 3건뿐」이라고 했으므로 이
// 신호는 마지막에 붙는 값이고, 없어도 나머지가 도는 꼴이어야 한다.
//
// **판정자가 아니다.** 코사인은 후보를 데려오기만 하고, 확정은 2단(통째 점수 S ·
// 사실 어긋남 검사)이 한다. e5 계열은 모든 쌍이 0.7 위로 뭉쳐 절대값 문턱이
// 뜻을 잃기 때문이다(결정 33).
type Vectors interface {
	// Near 는 이 기억과 벡터가 가까운 기억 id 를 가까운 차례로 돌려준다.
	// 벡터가 없는 기억이면 빈 목록을 돌려준다.
	Near(id string, limit int) []string
	// Cos 는 기억 둘의 코사인이다. 벡터가 없으면 0 이다.
	Cos(left, right string) float64
}

// UseVectors 는 셋째 신호를 꽂는다. nil 이면 안 쓴다.
func (s *Similar) UseVectors(near Vectors) { s.vectors = near }

// vectorLimit 은 기억 하나가 벡터로 데려올 후보 수다. 후보 상한(PerDoc 50) 안에
// 드는 값으로 잡는다.
const vectorLimit = 10
