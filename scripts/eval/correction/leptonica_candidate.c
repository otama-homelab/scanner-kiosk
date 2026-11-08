#include <leptonica/allheaders.h>

#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static const char* env(const char* name) {
    const char* value = getenv(name);
    return value ? value : "";
}

int main(void) {
    const char* input = env("SCANNER_EVAL_INPUT");
    const char* output = env("SCANNER_EVAL_OUTPUT");
    const char* original = env("SCANNER_EVAL_ORIGINAL_ID");
    const char* candidate_env = env("EVAL_CANDIDATE_ID");
    const char* candidate = candidate_env[0] ? candidate_env : "candidate_correction_leptonica_001";
    PIX* source = pixRead(input);
    if (!source) return 2;
    PIX* normalized = pixBackgroundNormSimple(source, NULL, NULL);
    if (!normalized) {
        pixDestroy(&source);
        return 3;
    }
    l_float32 angle = 0, confidence = 0;
    PIX* corrected = pixFindSkewAndDeskew(normalized, 2, &angle, &confidence);
    pixDestroy(&normalized);
    pixDestroy(&source);
    if (!corrected) return 4;
    char path[4096];
    snprintf(path, sizeof(path), "%s/corrected.png", output);
    if (pixWrite(path, corrected, IFF_PNG)) {
        pixDestroy(&corrected);
        return 5;
    }
    const l_int32 width = pixGetWidth(corrected);
    const l_int32 height = pixGetHeight(corrected);
    pixDestroy(&corrected);
    const char* pages = env("SCANNER_EVAL_SOURCE_PAGES");
    char resultPath[4096];
    snprintf(resultPath, sizeof(resultPath), "%s/result.json", output);
    FILE* result = fopen(resultPath, "w");
    if (!result) return 6;
    fprintf(result, "{\"format\":\"scanner-eval-candidate-result\",\"version\":1,");
    fprintf(result, "\"candidate_id\":\"%s\",\"artifacts\":[{", candidate);
    fprintf(result, "\"id\":\"artifact_candidate_correction_001\",\"role\":\"correction\",");
    fprintf(result, "\"path\":\"corrected.png\",\"media_type\":\"image/png\",");
    fprintf(result, "\"source_pages\":[");
    char pageBuffer[4096];
    snprintf(pageBuffer, sizeof(pageBuffer), "%s", pages);
    int first = 1;
    for (char* page = strtok(pageBuffer, ","); page; page = strtok(NULL, ",")) {
        fprintf(result, "%s\"%s\"", first ? "" : ",", page);
        first = 0;
    }
    fprintf(result, "],\"parents\":[\"%s\"],", original);
    fprintf(result, "\"algorithm\":\"leptonica-background-deskew\",\"algorithm_version\":\"1.87.0\",");
    fprintf(result, "\"parameters\":{\"crop_quad\":\"[[0,0],[%d,0],[%d,%d],[0,%d]]\",", width, width, height, height);
    fprintf(result, "\"rotation_degrees\":\"%.9g\",\"output_width\":\"%d\",\"output_height\":\"%d\",", angle, width, height);
    fprintf(result, "\"confidence_kind\":\"leptonica_skew_confidence\",\"confidence_value\":\"%.9g\",", confidence);
    fprintf(result, "\"fallback_reason\":\"%s\"}}]}\n", isfinite(angle) && angle != 0 ? "" : "deskew-not-required");
    return fclose(result) == 0 ? 0 : 7;
}
