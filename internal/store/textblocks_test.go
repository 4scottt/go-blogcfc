package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// TestTextblockCRUDAndMap: unique labels, and the one-query map the
// substitution pass uses.
func TestTextblockCRUDAndMap(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()

	sig := &store.Textblock{Label: "signature", Body: "<p>Ray</p>"}
	if err := st.CreateTextblock(ctx, sig); err != nil {
		t.Fatalf("CreateTextblock: %v", err)
	}
	if sig.ID == "" {
		t.Fatal("CreateTextblock did not fill the id")
	}
	if err := st.CreateTextblock(ctx, &store.Textblock{Label: "advert", Body: "buy"}); err != nil {
		t.Fatalf("CreateTextblock: %v", err)
	}
	if err := st.CreateTextblock(ctx, &store.Textblock{Label: "signature"}); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("a duplicate label gave %v, want ErrDuplicate", err)
	}

	blocks, err := st.ListTextblocks(ctx)
	if err != nil {
		t.Fatalf("ListTextblocks: %v", err)
	}
	if len(blocks) != 2 || blocks[0].Label != "advert" || blocks[1].Label != "signature" {
		t.Fatalf("blocks = %+v, want advert then signature", blocks)
	}

	got, err := st.GetTextblockByLabel(ctx, "signature")
	if err != nil || got.Body != "<p>Ray</p>" {
		t.Fatalf("GetTextblockByLabel = %+v (%v)", got, err)
	}
	if _, err := st.GetTextblockByLabel(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("an unknown label gave %v, want ErrNotFound", err)
	}

	sig.Body = "<p>Raymond</p>"
	if err := st.UpdateTextblock(ctx, sig); err != nil {
		t.Fatalf("UpdateTextblock: %v", err)
	}
	if err := st.UpdateTextblock(ctx, &store.Textblock{ID: "no-such-block", Label: "x"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("updating a missing block gave %v, want ErrNotFound", err)
	}

	m, err := st.TextblockMap(ctx)
	if err != nil {
		t.Fatalf("TextblockMap: %v", err)
	}
	if len(m) != 2 || m["signature"] != "<p>Raymond</p>" || m["advert"] != "buy" {
		t.Fatalf("TextblockMap = %v", m)
	}

	if err := st.DeleteTextblock(ctx, sig.ID); err != nil {
		t.Fatalf("DeleteTextblock: %v", err)
	}
	if _, err := st.GetTextblockByLabel(ctx, "signature"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the deleted block gave %v, want ErrNotFound", err)
	}
}
