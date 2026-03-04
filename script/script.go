package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"survey/structs"

	firebase "firebase.google.com/go"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

//go:embed firebaseKey.json
var firebaseKeyJSON []byte

func main() {
	// 실행파일의 디렉토리 경로 가져오기
	exePath, _ := os.Executable()

	// go run으로 실행되었는지 확인
	isGoRun := strings.Contains(exePath, "go-build")

	var exeDir string
	if isGoRun {
		exeDir = "/root/survey/script/"
	} else {
		exeDir = filepath.Dir(exePath)
	}

	if err := exportSurbey(exeDir); err != nil {
		fmt.Printf("\n오류 발생: %v\n", err)
	}

	fmt.Println("\n엔터를 누르면 종료됩니다...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

func exportSurbey(exeDir string) error {
	// Firestore 초기화
	ctx := context.Background()

	sa := option.WithAuthCredentialsJSON(option.ServiceAccount, firebaseKeyJSON)

	app, err := firebase.NewApp(ctx, nil, sa)
	if err != nil {
		return fmt.Errorf("Firebase 앱 초기화 실패: %w", err)
	}

	fmt.Println("1. Firebase 연동 완료 ")

	client, err := app.Firestore(ctx)
	if err != nil {
		return fmt.Errorf("Firestore 클라이언트 생성 실패: %w", err)
	}
	defer client.Close()

	fmt.Println("2. Firestore Database 연동 완료 ")

	// CSV 파일 생성
	filename := fmt.Sprintf("survey_export_%s.csv", time.Now().Format("20060102_150405"))
	filePath := filepath.Join(exeDir, filename)

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("CSV 파일 생성 실패: %w", err)
	}
	defer file.Close()

	fmt.Println("3. CSV 파일 생성 완료 ")

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// UTF-8 BOM 추가 (Excel에서 한글 깨짐 방지)
	file.Write([]byte{0xEF, 0xBB, 0xBF})

	var survey structs.Survey
	headers := []string{"문서ID", "제출 날짜", "C문항 유형"}

	for _, category := range []string{"A", "B", "C"} {
		v := reflect.ValueOf(survey)

		for i := 1; ; i++ {
			field := v.FieldByName(fmt.Sprintf("%s%d", category, i))
			if !field.IsValid() {
				break
			}

			if category == "C" {
				if c, ok := field.Interface().(structs.C); ok {
					headers = append(headers, c.PrintHeader(i)...)
				}
			} else {
				if p, ok := field.Interface().(interface{ PrintHeader() []string }); ok {
					headers = append(headers, p.PrintHeader()...)
				}
			}
		}
	}
	writer.Write(headers)

	fmt.Println("4. CSV 헤더 쓰기 완료 ")

	total := 0
	success := 0

	fmt.Println("5. Firestore 데이터 조회 시작...")
	
	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	
	iter := client.Collection("surveys").Documents(queryCtx)

	for {
		doc, err := iter.Next()
		if err != nil {
			if err == iterator.Done {
				break
			}
			return fmt.Errorf("문서 조회 실패: %w", err)
		}
		
		total++
		
		fmt.Printf("6-%3d. Document 불러오기 성공 \n", total)

		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("⚠️  문서 처리 중 panic 발생 (ID: %s): %v\n", doc.Ref.ID, r)
				}
			}()


			var survey structs.Survey
			if err := doc.DataTo(&survey); err != nil {
				fmt.Printf("⚠️  문서 데이터 파싱 실패 (ID: %s): %v\n", doc.Ref.ID, err)
				return
			}

			loc := time.FixedZone("KST", 9*60*60)
			createdAt := survey.CreatedAt.In(loc).Format("2006-01-02 15:04:05")

			questionStrs := make([]string, len(survey.WeightType))
			for i, q := range survey.WeightType {
				questionStrs[i] = strconv.Itoa(q)
			}

			row := []string{doc.Ref.ID, createdAt, strings.Join(questionStrs, ", ")}

			for _, category := range []string{"A", "B", "C"} {
				v := reflect.ValueOf(survey)

				for i := 1; ; i++ {
					field := v.FieldByName(fmt.Sprintf("%s%d", category, i))
					if !field.IsValid() {
						break
					}

					if p, ok := field.Interface().(structs.Question); ok {
						row = append(row, p.PrintString()...)
					}
				}
			}

			if err := writer.Write(row); err != nil {
				fmt.Printf("⚠️  CSV 쓰기 실패 (ID: %s): %v\n", doc.Ref.ID, err)
				return
			}

			success++
			fmt.Printf("6-%3d. Document 쓰기 성공 \n", total)
		}()
	}

	fmt.Printf("7. CSV 데이터 쓰기 완료(%d/%d)\n", success, total)

	return nil
}
