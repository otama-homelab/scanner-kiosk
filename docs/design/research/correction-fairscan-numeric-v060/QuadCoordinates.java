package diagnostic;

import java.nio.file.*;
import java.util.*;
import org.fairscan.evaluation.MatMask;
import org.fairscan.imageprocessing.*;
import org.opencv.core.Mat;
import org.opencv.imgcodecs.Imgcodecs;

public final class QuadCoordinates {
    private static String point(Point p) {
        if (!Double.isFinite(p.getX()) || !Double.isFinite(p.getY())) throw new IllegalStateException("Nonfinite official point");
        return "[" + p.getX() + "," + p.getY() + "]";
    }
    public static void main(String[] args) throws Exception {
        if (args.length != 2) throw new IllegalArgumentException("dataset-root output-json");
        nu.pattern.OpenCV.loadLocally();
        Path dataset = Paths.get(args[0]);
        List<Path> images;
        try (var files = Files.list(dataset.resolve("images"))) {
            images = files.filter(p -> p.toString().endsWith(".jpg") || p.toString().endsWith(".jpeg")).sorted().toList();
        }
        if (images.isEmpty()) throw new IllegalArgumentException("No inputs");
        List<String> rows = new ArrayList<>();
        for (Path path : images) {
            String id = path.getFileName().toString().replaceFirst("\\.[^.]+$", "");
            if (!id.matches("[A-Za-z0-9_-]+")) throw new IllegalArgumentException("Unsafe id");
            Mat image = Imgcodecs.imread(path.toString());
            Mat maskMat = Imgcodecs.imread(dataset.resolve("masks").resolve(id + ".png").toString(), Imgcodecs.IMREAD_UNCHANGED);
            if (image.empty() || maskMat.empty()) throw new IllegalArgumentException("Missing image or mask: " + id);
            MatMask mask = new MatMask(maskMat);
            Quad quad = DocumentDetectionKt.detectDocumentQuad(mask, new ImageSize((double) image.width(), (double) image.height()), Mode.CAPTURE);
            if (quad != null) quad = GeometryKt.scaledTo(quad, mask.getWidth(), mask.getHeight(), image.width(), image.height());
            String coords = quad == null ? "null" : "[" + point(quad.getTopLeft()) + "," + point(quad.getTopRight()) + "," + point(quad.getBottomRight()) + "," + point(quad.getBottomLeft()) + "]";
            rows.add("{\"id\":\"" + id + "\",\"width\":" + image.width() + ",\"height\":" + image.height() + ",\"quad\":" + coords + ",\"internal_fallback\":\"not exposed by official API\"}");
            System.out.println("Serialized official quad: " + id + " present=" + (quad != null));
            image.release(); maskMat.release();
        }
        Files.writeString(Paths.get(args[1]), "{\"rows\":[" + String.join(",", rows) + "]}\n");
    }
}
