import { OfficePage } from "@multica/views/office";

// NEX-1040/NEX-1045: direct-URL only for now, not in the sidebar — the
// NEX-1045 decision explicitly defers nav placement until this screen has
// been reviewed ("메뉴 순서 변경은 화면이 실제로 나온 뒤 적용").
export default function OfficeRoute() {
  return <OfficePage />;
}
