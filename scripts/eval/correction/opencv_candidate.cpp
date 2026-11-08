#include <opencv2/imgcodecs.hpp>
#include <opencv2/imgproc.hpp>

#include <cmath>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <sstream>
#include <string>

static std::string env(const char* name) {
    const char* value = std::getenv(name);
    return value ? value : "";
}

int main() {
    const std::string input = env("SCANNER_EVAL_INPUT");
    const std::string output = env("SCANNER_EVAL_OUTPUT");
    const std::string original = env("SCANNER_EVAL_ORIGINAL_ID");
    const std::string candidate = env("EVAL_CANDIDATE_ID").empty()
        ? "candidate_correction_opencv_001" : env("EVAL_CANDIDATE_ID");
    cv::Mat source = cv::imread(input, cv::IMREAD_COLOR);
    if (source.empty()) return 2;

    cv::Mat gray, mask;
    cv::cvtColor(source, gray, cv::COLOR_BGR2GRAY);
    cv::threshold(gray, mask, 245, 255, cv::THRESH_BINARY_INV);
    std::vector<cv::Point> points;
    cv::findNonZero(mask, points);
    cv::Rect crop(0, 0, source.cols, source.rows);
    std::string fallback;
    if (!points.empty()) {
        crop = cv::boundingRect(points);
        const int margin = 8;
        crop.x = std::max(0, crop.x - margin);
        crop.y = std::max(0, crop.y - margin);
        crop.width = std::min(source.cols - crop.x, crop.width + 2 * margin);
        crop.height = std::min(source.rows - crop.y, crop.height + 2 * margin);
    }
    if (crop.area() < source.total() / 100 ||
        crop.area() > source.total() * 99 / 100) {
        crop = cv::Rect(0, 0, source.cols, source.rows);
        fallback = "no-safe-crop";
    }
    cv::Mat corrected;
    cv::Mat matrix = cv::getPerspectiveTransform(
        std::vector<cv::Point2f>{
            {float(crop.x), float(crop.y)},
            {float(crop.x + crop.width), float(crop.y)},
            {float(crop.x + crop.width), float(crop.y + crop.height)},
            {float(crop.x), float(crop.y + crop.height)},
        },
        std::vector<cv::Point2f>{
            {0, 0}, {float(crop.width), 0},
            {float(crop.width), float(crop.height)}, {0, float(crop.height)},
        });
    cv::warpPerspective(source, corrected, matrix, crop.size());
    if (!cv::imwrite(output + "/corrected.png", corrected)) return 3;
    const double confidence = double(crop.area()) / double(source.total());
    std::ostringstream quad;
    quad << "[[" << crop.x << "," << crop.y << "],[" << crop.x + crop.width << ","
         << crop.y << "],[" << crop.x + crop.width << "," << crop.y + crop.height
         << "],[" << crop.x << "," << crop.y + crop.height << "]]";
    std::string pages = env("SCANNER_EVAL_SOURCE_PAGES");
    std::stringstream pageJSON;
    std::stringstream pageInput(pages);
    std::string page;
    bool first = true;
    pageJSON << "[";
    while (std::getline(pageInput, page, ',')) {
        if (page.empty()) continue;
        if (!first) pageJSON << ",";
        pageJSON << "\"" << page << "\"";
        first = false;
    }
    pageJSON << "]";
    std::ofstream result(output + "/result.json");
    result << "{\"format\":\"scanner-eval-candidate-result\",\"version\":1,"
           << "\"candidate_id\":\"" << candidate << "\",\"artifacts\":[{"
           << "\"id\":\"artifact_candidate_correction_001\",\"role\":\"correction\","
           << "\"path\":\"corrected.png\",\"media_type\":\"image/png\","
           << "\"source_pages\":" << pageJSON.str() << ",\"parents\":[\"" << original << "\"],"
           << "\"algorithm\":\"opencv-perspective-crop\",\"algorithm_version\":\"4.6.0\","
           << "\"parameters\":{\"crop_quad\":\"" << quad.str() << "\","
           << "\"rotation_degrees\":\"0\",\"output_width\":\"" << crop.width << "\","
           << "\"output_height\":\"" << crop.height << "\","
           << "\"confidence_kind\":\"opencv_crop_area_ratio\","
           << "\"confidence_value\":\"" << confidence << "\","
           << "\"fallback_reason\":\"" << fallback << "\"}}]}\n";
    return result ? 0 : 4;
}
