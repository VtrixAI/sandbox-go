package main

import (
	"context"
	"fmt"

	sdk "github.com/VtrixAI/sandbox-go/src"
)

// ────────────────────────────────────────────────────────────────────────────
// List tests
// ────────────────────────────────────────────────────────────────────────────

func testList(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: List ──")

	// List all
	res, err := client.List(ctx, sdk.ListOptions{})
	check("list all no error", err == nil, fmt.Sprint(err))
	if err == nil {
		check("list all items not nil", res.Items != nil)
		check("list pagination.total >= 0", res.Pagination.Total >= 0, fmt.Sprint(res.Pagination.Total))
		fmt.Printf("    total=%d items_in_page=%d\n", res.Pagination.Total, len(res.Items))
	}

	// List with limit=1
	res2, err := client.List(ctx, sdk.ListOptions{Limit: 1})
	check("list limit=1 no error", err == nil, fmt.Sprint(err))
	if err == nil {
		check("list limit=1 at most 1 item", len(res2.Items) <= 1, fmt.Sprint(len(res2.Items)))
		check("list limit=1 pagination.limit=1", res2.Pagination.Limit == 1, fmt.Sprint(res2.Pagination.Limit))
	}

	// List with offset
	res3, err := client.List(ctx, sdk.ListOptions{Limit: 10, Offset: 0})
	check("list offset=0 no error", err == nil, fmt.Sprint(err))
	if err == nil {
		check("list offset=0 pagination.offset=0", res3.Pagination.Offset == 0, fmt.Sprint(res3.Pagination.Offset))
	}

	// List with status filter
	res4, err := client.List(ctx, sdk.ListOptions{Status: "active"})
	check("list status=active no error", err == nil, fmt.Sprint(err))
	if err == nil {
		allActive := true
		for _, sb := range res4.Items {
			if sb.Status != "active" {
				allActive = false
			}
		}
		check("list status=active all items active", allActive)
	}

	// List with user_id filter — create a sandbox with a unique user, then filter by it
	tmpSB, err := client.Create(ctx, sdk.CreateOptions{UserID: "list-filter-user"})
	check("list user_id: create no error", err == nil, fmt.Sprint(err))
	if err == nil {
		res5, err5 := client.List(ctx, sdk.ListOptions{UserID: "list-filter-user"})
		check("list user_id filter no error", err5 == nil, fmt.Sprint(err5))
		if err5 == nil {
			found := false
			allMatch := true
			for _, item := range res5.Items {
				if item.ID == tmpSB.Info.ID {
					found = true
				}
				if item.UserID != "list-filter-user" {
					allMatch = false
				}
			}
			check("list user_id: target sandbox found", found)
			check("list user_id: all items match user_id", allMatch)
		}
		tmpSB.Close()
		client.Delete(ctx, tmpSB.Info.ID)
	}

	// HasMore: limit=1 with total > 1 should yield has_more=true
	resAll, errAll := client.List(ctx, sdk.ListOptions{})
	if errAll == nil && resAll.Pagination.Total > 1 {
		resHasMore, errHM := client.List(ctx, sdk.ListOptions{Limit: 1})
		check("list has_more: no error", errHM == nil, fmt.Sprint(errHM))
		if errHM == nil {
			check("list has_more=true when total>limit", resHasMore.Pagination.HasMore,
				fmt.Sprintf("total=%d limit=1 has_more=%v", resAll.Pagination.Total, resHasMore.Pagination.HasMore))
		}
	} else {
		fmt.Println("    SKIPPED has_more check (total <= 1 or list error)")
	}

	// Offset beyond total — items should be empty
	resAll2, errAll2 := client.List(ctx, sdk.ListOptions{})
	if errAll2 == nil {
		bigOffset := resAll2.Pagination.Total + 9999
		resOOB, errOOB := client.List(ctx, sdk.ListOptions{Limit: 10, Offset: bigOffset})
		check("list offset>total no error", errOOB == nil, fmt.Sprint(errOOB))
		if errOOB == nil {
			check("list offset>total: empty items", len(resOOB.Items) == 0,
				fmt.Sprintf("got %d items", len(resOOB.Items)))
			check("list offset>total: has_more=false", !resOOB.Pagination.HasMore,
				fmt.Sprintf("has_more=%v", resOOB.Pagination.HasMore))
		}
	}

	// Invalid status value — server should return error or empty list, not panic
	resInv, errInv := client.List(ctx, sdk.ListOptions{Status: "nonexistent-status"})
	// Both behaviours acceptable; we only ensure no panic and record the result
	fmt.Printf("    list status=nonexistent: err=%v items=%d\n", errInv, func() int {
		if resInv != nil {
			return len(resInv.Items)
		}
		return -1
	}())
	check("list invalid status: did not panic", true)
	if errInv == nil && resInv != nil {
		check("list invalid status: empty items", len(resInv.Items) == 0,
			fmt.Sprintf("%d items", len(resInv.Items)))
	}
}

func testListCombinedFilter(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: List with combined UserID+Status filter ──")

	sbA, errA := client.Create(ctx, sdk.CreateOptions{UserID: "list-combined-userA"})
	check("list-combined: create userA no error", errA == nil, fmt.Sprint(errA))
	sbB, errB := client.Create(ctx, sdk.CreateOptions{UserID: "list-combined-userB"})
	check("list-combined: create userB no error", errB == nil, fmt.Sprint(errB))

	defer func() {
		if sbA != nil {
			sbA.Close()
			client.Delete(ctx, sbA.Info.ID)
		}
		if sbB != nil {
			sbB.Close()
			client.Delete(ctx, sbB.Info.ID)
		}
	}()

	if errA != nil || errB != nil {
		return
	}

	res, err := client.List(ctx, sdk.ListOptions{
		UserID: "list-combined-userA",
		Status: "active",
	})
	check("list-combined: no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}

	foundA := false
	allMatch := true
	for _, item := range res.Items {
		if item.ID == sbA.Info.ID {
			foundA = true
		}
		if item.UserID != "list-combined-userA" || item.Status != "active" {
			allMatch = false
		}
	}
	check("list-combined: userA sandbox found", foundA)
	check("list-combined: all items match UserID+Status", allMatch)

	// userB sandbox must NOT appear in userA-filtered results
	foundB := false
	for _, item := range res.Items {
		if sbB != nil && item.ID == sbB.Info.ID {
			foundB = true
		}
	}
	check("list-combined: userB sandbox not in userA results", !foundB)
	fmt.Printf("    list-combined: found=%d foundA=%v foundB=%v\n", len(res.Items), foundA, foundB)
}

func testListPagination(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: List pagination (no duplicate across pages) ──")

	// Create 3 sandboxes to ensure enough items for 2 pages of 2
	var created []*sdk.Sandbox
	for i := 0; i < 3; i++ {
		sb, err := client.Create(ctx, sdk.CreateOptions{
			UserID: fmt.Sprintf("list-pagination-user-%d", i),
			Labels: map[string]string{"suite": "pagination"},
		})
		check(fmt.Sprintf("list-pagination: create %d no error", i), err == nil, fmt.Sprint(err))
		if err == nil {
			created = append(created, sb)
		}
	}
	defer func() {
		for _, sb := range created {
			sb.Close()
			client.Delete(ctx, sb.Info.ID)
		}
	}()

	page1, err1 := client.List(ctx, sdk.ListOptions{Limit: 2, Offset: 0})
	check("list-pagination: page1 no error", err1 == nil, fmt.Sprint(err1))

	page2, err2 := client.List(ctx, sdk.ListOptions{Limit: 2, Offset: 2})
	check("list-pagination: page2 no error", err2 == nil, fmt.Sprint(err2))

	if err1 != nil || err2 != nil {
		return
	}

	// No ID should appear on both pages
	page1IDs := make(map[string]bool)
	for _, item := range page1.Items {
		page1IDs[item.ID] = true
	}
	overlap := false
	for _, item := range page2.Items {
		if page1IDs[item.ID] {
			overlap = true
		}
	}
	check("list-pagination: no duplicate IDs across pages", !overlap,
		fmt.Sprintf("page1=%d page2=%d", len(page1.Items), len(page2.Items)))

	// Total should be consistent between both calls
	check("list-pagination: total consistent across pages",
		page1.Pagination.Total == page2.Pagination.Total,
		fmt.Sprintf("page1.total=%d page2.total=%d", page1.Pagination.Total, page2.Pagination.Total))

	fmt.Printf("    list-pagination: total=%d page1=%d page2=%d\n",
		page1.Pagination.Total, len(page1.Items), len(page2.Items))
}

func testListLimitZero(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: List with Limit=0 (server default) ──")

	res, err := client.List(ctx, sdk.ListOptions{Limit: 0})
	check("list-limit-zero: no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	check("list-limit-zero: items not nil", res.Items != nil)
	check("list-limit-zero: pagination.limit >= 0", res.Pagination.Limit >= 0, fmt.Sprint(res.Pagination.Limit))
	fmt.Printf("    list-limit-zero: returned limit=%d total=%d items=%d\n",
		res.Pagination.Limit, res.Pagination.Total, len(res.Items))
}
