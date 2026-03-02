package identity

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestFollowUser(t *testing.T) {

	// 1️⃣ Create mock database
	dbMock, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error creating sqlmock: %v", err)
	}
	defer dbMock.Close()

	// Inject mock DB
	SetDB(dbMock)

	follower := "userA"
	followee := "userB"

	// 2️⃣ Expect INSERT into follows table
	mock.ExpectExec("INSERT INTO follows").
		WithArgs(follower, sqlmock.AnyArg(), followee, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// 3️⃣ Expect LogActivity INSERT
	mock.ExpectExec("INSERT INTO activities").
		WithArgs(follower, "FOLLOW", "user", followee, "", "{}").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// 4️⃣ Expect CreateNotification INSERT
	mock.ExpectExec("INSERT INTO notifications").
		WithArgs(followee, follower, "FOLLOW", followee, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// 5️⃣ Call function
	err = FollowUser(follower, followee)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// 6️⃣ Verify expectations
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
